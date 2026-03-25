package policy

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

type weightedMs map[int]float64

type procRules map[string]weightedMs

type rawPolicy struct {
	RPCDelay map[string]procRules `json:"__rpc_delay__"`
	RPCDrop  map[string]procRules `json:"__rpc_drop__"`
}

type Action struct {
	DelayMs int
	Drop    bool
}

type Manager struct {
	mu     sync.RWMutex
	policy rawPolicy
	rng    *rand.Rand
}

func NewManager() *Manager {
	return &Manager{
		policy: rawPolicy{
			RPCDelay: map[string]procRules{},
			RPCDrop:  map[string]procRules{},
		},
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func normalizeRuleType(ruleType string) (string, error) {
	switch ruleType {
	case "__rpc_delay__", "delay", "rpc_delay":
		return "__rpc_delay__", nil
	case "__rpc_drop__", "drop", "rpc_drop":
		return "__rpc_drop__", nil
	default:
		return "", fmt.Errorf("unsupported rule type %q", ruleType)
	}
}

func (m *Manager) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return m.LoadBytes(data)
}

func (m *Manager) LoadBytes(data []byte) error {
	var src map[string]any
	if err := json.Unmarshal(data, &src); err != nil {
		return err
	}

	out := rawPolicy{
		RPCDelay: map[string]procRules{},
		RPCDrop:  map[string]procRules{},
	}

	if v, ok := src["__rpc_delay__"]; ok {
		rules, err := parseProcRules(v)
		if err != nil {
			return fmt.Errorf("parse __rpc_delay__: %w", err)
		}
		out.RPCDelay = rules
	}
	if v, ok := src["__rpc_drop__"]; ok {
		rules, err := parseProcRules(v)
		if err != nil {
			return fmt.Errorf("parse __rpc_drop__: %w", err)
		}
		out.RPCDrop = rules
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.policy = out
	return nil
}

func (m *Manager) Snapshot() map[string]map[string]procRules {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := map[string]map[string]procRules{
		"__rpc_delay__": {},
		"__rpc_drop__":  {},
	}
	copyRules := func(src map[string]procRules) map[string]procRules {
		dst := map[string]procRules{}
		for proc, clientRules := range src {
			dstClientRules := procRules{}
			for client, weights := range clientRules {
				dstWeights := weightedMs{}
				for ms, share := range weights {
					dstWeights[ms] = share
				}
				dstClientRules[client] = dstWeights
			}
			dst[proc] = dstClientRules
		}
		return dst
	}
	out["__rpc_delay__"] = copyRules(m.policy.RPCDelay)
	out["__rpc_drop__"] = copyRules(m.policy.RPCDrop)
	return out
}

func (m *Manager) JSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := map[string]map[string]map[string]map[string]float64{
		"__rpc_delay__": {},
		"__rpc_drop__":  {},
	}

	serialize := func(src map[string]procRules) map[string]map[string]map[string]float64 {
		dst := map[string]map[string]map[string]float64{}
		procs := make([]string, 0, len(src))
		for proc := range src {
			procs = append(procs, proc)
		}
		sort.Strings(procs)
		for _, proc := range procs {
			clientRules := src[proc]
			dst[proc] = map[string]map[string]float64{}
			clients := make([]string, 0, len(clientRules))
			for client := range clientRules {
				clients = append(clients, client)
			}
			sort.Strings(clients)
			for _, client := range clients {
				weights := clientRules[client]
				msKeys := make([]int, 0, len(weights))
				for ms := range weights {
					msKeys = append(msKeys, ms)
				}
				sort.Ints(msKeys)
				cw := map[string]float64{}
				for _, ms := range msKeys {
					cw[strconv.Itoa(ms)] = weights[ms]
				}
				dst[proc][client] = cw
			}
		}
		return dst
	}

	out["__rpc_delay__"] = serialize(m.policy.RPCDelay)
	out["__rpc_drop__"] = serialize(m.policy.RPCDrop)
	return json.MarshalIndent(out, "", "  ")
}

func (m *Manager) SetRule(ruleType, procedure, client string, weights map[int]float64) error {
	key, err := normalizeRuleType(ruleType)
	if err != nil {
		return err
	}
	if procedure == "" {
		return fmt.Errorf("procedure is required")
	}
	if client == "" {
		client = "default"
	}
	if len(weights) == 0 {
		return fmt.Errorf("weights must not be empty")
	}
	for ms, share := range weights {
		if ms < 0 {
			return fmt.Errorf("delay must be >= 0")
		}
		if share < 0 {
			return fmt.Errorf("share must be >= 0")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	target := m.policy.RPCDelay
	if key == "__rpc_drop__" {
		target = m.policy.RPCDrop
	}
	if _, ok := target[procedure]; !ok {
		target[procedure] = procRules{}
	}
	dstWeights := weightedMs{}
	for ms, share := range weights {
		dstWeights[ms] = share
	}
	target[procedure][client] = dstWeights
	return nil
}

func (m *Manager) DeleteRule(ruleType, procedure, client string) error {
	key, err := normalizeRuleType(ruleType)
	if err != nil {
		return err
	}
	if procedure == "" {
		return fmt.Errorf("procedure is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	target := m.policy.RPCDelay
	if key == "__rpc_drop__" {
		target = m.policy.RPCDrop
	}
	if _, ok := target[procedure]; !ok {
		return nil
	}

	if client == "" {
		delete(target, procedure)
		return nil
	}

	delete(target[procedure], client)
	if len(target[procedure]) == 0 {
		delete(target, procedure)
	}
	return nil
}

func parseProcRules(v any) (map[string]procRules, error) {
	root, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object")
	}
	out := map[string]procRules{}
	for proc, cv := range root {
		clientObj, ok := cv.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("proc %s: expected client object", proc)
		}
		clientRules := procRules{}
		for client, wv := range clientObj {
			weightsObj, ok := wv.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("proc %s client %s: expected weight map", proc, client)
			}
			weights := weightedMs{}
			for msStr, shareV := range weightsObj {
				var ms int
				if _, err := fmt.Sscanf(msStr, "%d", &ms); err != nil || ms < 0 {
					return nil, fmt.Errorf("proc %s client %s: bad delay key %q", proc, client, msStr)
				}
				share, ok := toFloat64(shareV)
				if !ok || share < 0 {
					return nil, fmt.Errorf("proc %s client %s: bad share for %q", proc, client, msStr)
				}
				weights[ms] = share
			}
			clientRules[client] = weights
		}
		out[proc] = clientRules
	}
	return out, nil
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}

func weightedPick(r *rand.Rand, in weightedMs) (int, bool) {
	total := 0.0
	for _, w := range in {
		total += w
	}
	if total <= 0 {
		return 0, false
	}
	target := r.Float64() * total
	upto := 0.0
	for ms, w := range in {
		upto += w
		if upto >= target {
			return ms, true
		}
	}
	return 0, false
}

func (m *Manager) lookup(ruleSet map[string]procRules, procName, clientIP string) (int, bool) {
	proc, ok := ruleSet[procName]
	if !ok {
		return 0, false
	}
	weights, ok := proc[clientIP]
	if !ok {
		weights = proc["default"]
	}
	if len(weights) == 0 {
		return 0, false
	}
	return weightedPick(m.rng, weights)
}

func (m *Manager) ActionFor(procName, clientIP string) Action {
	m.mu.RLock()
	defer m.mu.RUnlock()

	act := Action{}
	if ms, ok := m.lookup(m.policy.RPCDelay, procName, clientIP); ok {
		act.DelayMs = ms
	}
	if ms, ok := m.lookup(m.policy.RPCDrop, procName, clientIP); ok {
		act.DelayMs = ms
		act.Drop = true
	}
	return act
}
