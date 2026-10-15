package paper

import "time"

type ShadowTrial struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	BaselineID string `json:"baseline_id,omitempty"`
	Policy     string `json:"policy"`
	Config     Config `json:"config"`
}

type ShadowTrialSet struct {
	EngineVersion string        `json:"engine_version"`
	Version       int           `json:"version"`
	CreatedAt     time.Time     `json:"created_at"`
	Hash          string        `json:"hash"`
	Trials        []ShadowTrial `json:"trials"`
}
