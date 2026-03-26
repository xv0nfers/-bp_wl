package dpi_evasion

import (
	"math/rand"
	"time"
)

type Config struct {
	MimicVK    bool
	PaddingMax int
	JitterMs   int
}

type Evasion struct {
	cfg Config
	rng *rand.Rand
}

func New(cfg Config) *Evasion {
	if cfg.PaddingMax < 0 {
		cfg.PaddingMax = 0
	}
	if cfg.JitterMs < 0 {
		cfg.JitterMs = 0
	}
	return &Evasion{cfg: cfg, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (e *Evasion) Transform(p []byte) []byte {
	if e.cfg.PaddingMax <= 0 {
		return p
	}
	pad := e.rng.Intn(e.cfg.PaddingMax + 1)
	if e.cfg.MimicVK {
		target := 320 + e.rng.Intn(880)
		if len(p)+pad < target {
			pad += target - (len(p) + pad)
		}
	}
	out := make([]byte, len(p)+pad)
	copy(out, p)
	for i := len(p); i < len(out); i++ {
		out[i] = byte(e.rng.Intn(256))
	}
	return out
}

func (e *Evasion) Delay() {
	min, max := 10, e.cfg.JitterMs
	if e.cfg.MimicVK {
		min = 20
		if max < 80 {
			max = 80
		}
	}
	if max <= 0 {
		return
	}
	if max < min {
		max = min
	}
	d := min + e.rng.Intn(max-min+1)
	time.Sleep(time.Duration(d) * time.Millisecond)
}
