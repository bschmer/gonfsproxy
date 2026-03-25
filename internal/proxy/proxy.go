package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"gonfs_proxy/internal/metrics"
	"gonfs_proxy/internal/policy"
	"gonfs_proxy/internal/rpcx"
)

type Server struct {
	BackendAddr      string
	Policy           *policy.Manager
	Metrics          *metrics.Set
	Logger           *log.Logger
	Verbose          bool
	SecureSourcePort bool
	SourcePortMin    int
	SourcePortMax    int
}

type pendingAction struct {
	Procedure string
	DelayMs   int
	Drop      bool
}

type callMeta struct {
	Program uint32
	Version uint32
	Proc    uint32
	Name    string
	SentAt  time.Time
}

type rpcReplySummary struct {
	ReplyStat   uint32
	AcceptStat  *uint32
	NFSStatus   *uint32
	DecodeError string
}

func parseRPCReplySummary(payload []byte, meta callMeta) rpcReplySummary {
	out := rpcReplySummary{}
	if len(payload) < 12 {
		out.DecodeError = "short reply"
		return out
	}
	out.ReplyStat = binary.BigEndian.Uint32(payload[8:12])
	if out.ReplyStat != 0 {
		return out
	}
	// accepted_reply: opaque_auth verf + accept_stat + results
	p := 12
	if p+8 > len(payload) {
		out.DecodeError = "short verf header"
		return out
	}
	p += 4 // verf flavor
	vlen := int(binary.BigEndian.Uint32(payload[p : p+4]))
	p += 4
	if vlen < 0 || p+vlen > len(payload) {
		out.DecodeError = "bad verf length"
		return out
	}
	p += vlen
	p += (4 - (vlen % 4)) % 4
	if p+4 > len(payload) {
		out.DecodeError = "short accept stat"
		return out
	}
	accept := binary.BigEndian.Uint32(payload[p : p+4])
	out.AcceptStat = &accept
	p += 4

	// If RPC SUCCESS and this was NFS, first u32 of results is NFS status for v3.
	if meta.Program == rpcx.NFSProgram && accept == 0 && p+4 <= len(payload) {
		st := binary.BigEndian.Uint32(payload[p : p+4])
		out.NFSStatus = &st
	}
	return out
}

func (s *Server) Serve(listener net.Listener) error {
	if s.Logger == nil {
		s.Logger = log.Default()
	}
	for {
		clientConn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(clientConn)
	}
}

func (s *Server) handleConn(client net.Conn) {
	defer client.Close()

	backend, err := s.dialBackend()
	if err != nil {
		s.Logger.Printf("backend dial failed: %v", err)
		return
	}
	defer backend.Close()
	if s.Verbose {
		s.Logger.Printf("proxy conn client=%s backend=%s established", client.RemoteAddr().String(), s.BackendAddr)
	}

	clientIP, _, _ := net.SplitHostPort(client.RemoteAddr().String())
	if clientIP == "" {
		clientIP = "default"
	}

	var (
		wg       sync.WaitGroup
		xidMu    sync.Mutex
		pending  = map[uint32]pendingAction{}
		inflight = map[uint32]callMeta{}
	)

	closeBoth := func() {
		_ = client.Close()
		_ = backend.Close()
		if s.Verbose {
			s.Logger.Printf("proxy conn client=%s backend=%s closed", client.RemoteAddr().String(), s.BackendAddr)
		}
	}

	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			rec, err := rpcx.ReadRecord(client)
			if err != nil {
				if err != io.EOF {
					s.Logger.Printf("client read record error: %v", err)
				}
				return
			}

			h, err := rpcx.ParseCallHeader(rec)
			if err == nil {
				proc := rpcx.ProcNameFor(h.Program, h.Proc)
				xidMu.Lock()
				inflight[h.XID] = callMeta{
					Program: h.Program,
					Version: h.Version,
					Proc:    h.Proc,
					Name:    proc,
					SentAt:  time.Now(),
				}
				xidMu.Unlock()
				if s.Verbose {
					s.Logger.Printf("forward call xid=%d prog=%d vers=%d proc=%d(%s) -> backend=%s", h.XID, h.Program, h.Version, h.Proc, proc, s.BackendAddr)
				}
			} else if s.Verbose {
				s.Logger.Printf("forward call unparsed payload_len=%d -> backend=%s", len(rec), s.BackendAddr)
			}

			if err == nil && h.Program == rpcx.NFSProgram {
				proc := rpcx.ProcName(h.Proc)
				act := policy.Action{}
				if s.Policy != nil {
					act = s.Policy.ActionFor(proc, clientIP)
				}
				if s.Verbose {
					s.Logger.Printf("call xid=%d proc=%s client=%s action={delay_ms:%d drop:%v}", h.XID, proc, clientIP, act.DelayMs, act.Drop)
				}
				if s.Metrics != nil {
					s.Metrics.CallForwardedTotal.WithLabelValues(proc).Inc()
				}
				if act.DelayMs > 0 || act.Drop {
					xidMu.Lock()
					pending[h.XID] = pendingAction{
						Procedure: proc,
						DelayMs:   act.DelayMs,
						Drop:      act.Drop,
					}
					xidMu.Unlock()
				}
			}

			if err := rpcx.WriteRecord(backend, rec); err != nil {
				s.Logger.Printf("forward call write error: %v", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			rec, err := rpcx.ReadRecord(backend)
			if err != nil {
				if err != io.EOF {
					s.Logger.Printf("backend read record error: %v", err)
				}
				return
			}

			rh, err := rpcx.ParseReplyHeader(rec)
			if err == nil {
				var (
					meta      callMeta
					hadMeta   bool
					backendRT time.Duration
				)
				xidMu.Lock()
				meta, hadMeta = inflight[rh.XID]
				if hadMeta {
					delete(inflight, rh.XID)
					backendRT = time.Since(meta.SentAt)
				}
				xidMu.Unlock()
				if hadMeta && s.Metrics != nil {
					s.Metrics.RPCBackendRoundTripSeconds.WithLabelValues(fmt.Sprintf("%d", meta.Program), meta.Name).Observe(backendRT.Seconds())
				}
				if s.Verbose {
					if hadMeta {
						summary := parseRPCReplySummary(rec, meta)
						rpcStat := fmt.Sprintf("reply_stat=%d", summary.ReplyStat)
						if summary.AcceptStat != nil {
							rpcStat += fmt.Sprintf(" accept_stat=%d", *summary.AcceptStat)
						}
						nfsStat := ""
						if summary.NFSStatus != nil {
							nfsStat = fmt.Sprintf(" nfs_status=%d", *summary.NFSStatus)
						}
						if summary.DecodeError != "" {
							rpcStat += fmt.Sprintf(" decode_err=%q", summary.DecodeError)
						}
						s.Logger.Printf("forward reply xid=%d for prog=%d vers=%d proc=%d(%s) backend_rtt=%s %s%s", rh.XID, meta.Program, meta.Version, meta.Proc, meta.Name, backendRT.String(), rpcStat, nfsStat)
					} else {
						s.Logger.Printf("forward reply xid=%d (no matching inflight meta)", rh.XID)
					}
				}

				xidMu.Lock()
				act, ok := pending[rh.XID]
				if ok {
					delete(pending, rh.XID)
				}
				xidMu.Unlock()

				if ok && act.DelayMs > 0 {
					mode := "reply"
					if act.Drop {
						mode = "drop"
					}
					if s.Metrics != nil {
						s.Metrics.RPCDelayAppliedTotal.WithLabelValues(act.Procedure).Inc()
						delaySeconds := float64(act.DelayMs) / 1000.0
						s.Metrics.RPCDelaySecondsTotal.WithLabelValues(act.Procedure, mode).Add(delaySeconds)
						s.Metrics.RPCDelayInjectedSeconds.WithLabelValues(act.Procedure, mode).Observe(delaySeconds)
					}
					time.Sleep(time.Duration(act.DelayMs) * time.Millisecond)
				}

				if ok && act.Drop {
					if s.Metrics != nil {
						s.Metrics.RPCDropAppliedTotal.WithLabelValues(act.Procedure).Inc()
					}
					if s.Verbose {
						s.Logger.Printf("drop injected xid=%d proc=%s delay_ms=%d", rh.XID, act.Procedure, act.DelayMs)
					}
					closeBoth()
					return
				}

				if hadMeta && s.Metrics != nil {
					clientVisibleRT := time.Since(meta.SentAt)
					s.Metrics.RPCRoundTripSeconds.WithLabelValues(fmt.Sprintf("%d", meta.Program), meta.Name).Observe(clientVisibleRT.Seconds())
				}
			}

			if err := rpcx.WriteRecord(client, rec); err != nil {
				s.Logger.Printf("forward reply write error: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}

func (s *Server) dialBackend() (net.Conn, error) {
	if !s.SecureSourcePort {
		return net.Dial("tcp", s.BackendAddr)
	}

	host, portStr, err := net.SplitHostPort(s.BackendAddr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	backendIP := net.ParseIP(host)
	if backendIP == nil {
		ips, rerr := net.LookupIP(host)
		if rerr != nil || len(ips) == 0 {
			return nil, fmt.Errorf("backend host resolution failed for secure bind: %s", host)
		}
		backendIP = ips[0]
	}
	isV6 := backendIP.To4() == nil
	network := "tcp4"
	localIP := net.IPv4zero
	remoteIP := backendIP.To4()
	if isV6 {
		network = "tcp6"
		localIP = net.IPv6unspecified
		remoteIP = backendIP
	}

	minPort := s.SourcePortMin
	maxPort := s.SourcePortMax
	if minPort <= 0 {
		minPort = 665
	}
	if maxPort <= 0 {
		maxPort = 1023
	}
	if minPort > maxPort {
		minPort, maxPort = maxPort, minPort
	}

	var lastErr error
	for p := minPort; p <= maxPort; p++ {
		d := net.Dialer{
			Timeout: 3 * time.Second,
			LocalAddr: &net.TCPAddr{
				IP:   localIP,
				Port: p,
			},
		}
		conn, derr := d.Dial(network, net.JoinHostPort(remoteIP.String(), strconv.Itoa(port)))
		if derr == nil {
			if s.Verbose {
				s.Logger.Printf("backend secure bind success local_port=%d backend=%s", p, s.BackendAddr)
			}
			return conn, nil
		}
		lastErr = derr
	}
	return nil, fmt.Errorf("secure bind connect failed across %d-%d to %s: %w", minPort, maxPort, s.BackendAddr, lastErr)
}
