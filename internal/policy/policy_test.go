package policy

import "testing"

func TestActionForDefaultDelay(t *testing.T) {
	pm := NewManager()
	data := []byte(`{
	  "__rpc_delay__": {
	    "READ": {
	      "default": {
	        "1000": 1
	      }
	    }
	  }
	}`)
	if err := pm.LoadBytes(data); err != nil {
		t.Fatalf("LoadBytes failed: %v", err)
	}

	act := pm.ActionFor("READ", "10.1.2.3")
	if act.DelayMs != 1000 {
		t.Fatalf("unexpected DelayMs: got %d want %d", act.DelayMs, 1000)
	}
	if act.Drop {
		t.Fatalf("unexpected Drop: got true want false")
	}
}

func TestDropOverridesDelay(t *testing.T) {
	pm := NewManager()
	data := []byte(`{
	  "__rpc_delay__": {
	    "READDIRPLUS": {
	      "default": {
	        "250": 1
	      }
	    }
	  },
	  "__rpc_drop__": {
	    "READDIRPLUS": {
	      "default": {
	        "10000": 1
	      }
	    }
	  }
	}`)
	if err := pm.LoadBytes(data); err != nil {
		t.Fatalf("LoadBytes failed: %v", err)
	}

	act := pm.ActionFor("READDIRPLUS", "10.9.8.7")
	if act.DelayMs != 10000 {
		t.Fatalf("unexpected DelayMs: got %d want %d", act.DelayMs, 10000)
	}
	if !act.Drop {
		t.Fatalf("unexpected Drop: got false want true")
	}
}

func TestSetAndDeleteRule(t *testing.T) {
	pm := NewManager()
	if err := pm.SetRule("__rpc_delay__", "READ", "default", map[int]float64{1000: 1}); err != nil {
		t.Fatalf("SetRule failed: %v", err)
	}
	act := pm.ActionFor("READ", "198.51.100.10")
	if act.DelayMs != 1000 || act.Drop {
		t.Fatalf("unexpected action after SetRule: %+v", act)
	}

	if err := pm.DeleteRule("__rpc_delay__", "READ", "default"); err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}
	act = pm.ActionFor("READ", "198.51.100.10")
	if act.DelayMs != 0 || act.Drop {
		t.Fatalf("unexpected action after DeleteRule: %+v", act)
	}
}
