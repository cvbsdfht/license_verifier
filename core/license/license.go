package license

import (
	"crypto/ed25519"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

type LicenseInterface interface {
	Start()
	Stop()
	FiberMiddleware() fiber.Handler
	StatusHandler() fiber.Handler
}

type license struct {
	cfg     LicenseConfig
	pub     ed25519.PublicKey
	guard   *clockGuard
	mu      sync.RWMutex
	state   State
	stop    chan struct{}
	started sync.Once
	stopped sync.Once
}

type LicenseConfig struct {
	PublicKeyPEM     string        // baked at build time (never read from customer-editable env)
	DeploymentId     string        // baked — this customer's deployment_id
	BuildTime        time.Time     // baked — image build time
	TokenPath        string        // token file mounted by the customer (Secret)
	ClockFile        string        // high-water mark (must be persistent and writable)
	ExpiringSoonDays int           // default 30 (days)
	RecheckInterval  time.Duration // default 1 minute (recheck + reload token)
}

func New(cfg LicenseConfig) (LicenseInterface, error) {
	if cfg.PublicKeyPEM == "" {
		return nil, errors.New("PublicKeyPEM is required")
	}

	if cfg.DeploymentId == "" {
		return nil, errors.New("DeploymentId is required")
	}

	pub, err := parsePublicKey(cfg.PublicKeyPEM)
	if err != nil {
		return nil, err
	}

	if cfg.ExpiringSoonDays <= 0 {
		cfg.ExpiringSoonDays = 30
	}

	if cfg.RecheckInterval <= 0 {
		cfg.RecheckInterval = time.Minute
	}

	return &license{
		cfg: cfg,
		pub: pub,
		guard: &clockGuard{
			clockFileName: cfg.ClockFile,
			buildTime:     cfg.BuildTime,
			tolerance:     24 * time.Hour,
		},
		state: State{
			Status: "UNINITIALIZED",
		},
		stop: make(chan struct{}),
	}, nil
}

func (r *license) Start() {
	r.started.Do(func() {
		r.evaluate()

		go func() {
			t := time.NewTicker(r.cfg.RecheckInterval)
			defer t.Stop()

			for {
				select {
				case <-t.C:
					r.evaluate()
				case <-r.stop:
					return
				}
			}
		}()
	})
}

func (r *license) Stop() {
	r.stopped.Do(func() {
		close(r.stop)
	})
}

func (r *license) getState() State {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.state
}

func (r *license) isServing() bool {
	return isServingState(r.getState().Status)
}

// evaluate is only ever called by the single ticker goroutine in Start(),
// so no extra locking is needed here.
func (r *license) evaluate() {
	tokenFile, err := os.ReadFile(r.cfg.TokenPath)
	if err != nil {
		state := State{
			Status: STATUS_INVALID,
			Reason: "TOKEN_FILE_MISSING",
		}

		r.setState(state)
		return
	}

	now, tampered := r.guard.check(time.Now())
	if tampered {
		state := State{
			Status: STATUS_INVALID,
			Reason: "CLOCK_TAMPERED",
		}

		r.setState(state)
		return
	}

	token := strings.TrimSpace(string(tokenFile))
	state := verify(token, r.pub, r.cfg.DeploymentId, now, r.cfg.ExpiringSoonDays)
	r.setState(state)
}

func (r *license) setState(s State) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.state = s

	if s.Status != r.state.Status || s.Reason != r.state.Reason {
		log.Printf("[LICENSE] change to %s %s", s.Status, s.Reason)
	}
}
