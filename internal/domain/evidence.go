package domain

import "time"

type EvidenceKind string

const (
	EvidenceOfficialDisclosure EvidenceKind = "official_disclosure"
	EvidenceDisclosureIndex    EvidenceKind = "disclosure_index"
	EvidenceBrokerResearch     EvidenceKind = "broker_research"
	EvidenceAuthoritativeNews  EvidenceKind = "authoritative_news"
	EvidenceMarketCommentary   EvidenceKind = "market_commentary"
)

type EvidenceTier string

const (
	EvidenceTierA EvidenceTier = "A"
	EvidenceTierB EvidenceTier = "B"
	EvidenceTierC EvidenceTier = "C"
	EvidenceTierD EvidenceTier = "D"
)

// ResearchEvidence is a cited external source frozen with a report. Tier and
// Usage define what the source may support; an unverified index never becomes
// an announcement fact merely because it points at an official disclosure.
type ResearchEvidence struct {
	ID           string       `json:"id"`
	Kind         EvidenceKind `json:"kind"`
	Tier         EvidenceTier `json:"tier"`
	Symbol       string       `json:"symbol,omitempty"`
	Name         string       `json:"name,omitempty"`
	Title        string       `json:"title"`
	Publisher    string       `json:"publisher"`
	Author       string       `json:"author,omitempty"`
	PublishedAt  string       `json:"published_at,omitempty"`
	FetchedAt    time.Time    `json:"fetched_at"`
	URL          string       `json:"url,omitempty"`
	SourceID     string       `json:"source_id,omitempty"`
	Summary      string       `json:"summary,omitempty"`
	Rating       string       `json:"rating,omitempty"`
	VerifiedBody bool         `json:"verified_body"`
	Usage        string       `json:"usage"`
}

type EvidenceSnapshot struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Items       []ResearchEvidence `json:"items"`
	Warnings    []string           `json:"warnings,omitempty"`
}

// BrokerResearchItem is a publisher's opinion record. Forecasts and ratings
// are not company disclosures and are deliberately kept separate from facts.
type BrokerResearchItem struct {
	Symbol         string `json:"symbol"`
	Name           string `json:"name"`
	Title          string `json:"title"`
	Organization   string `json:"organization"`
	Author         string `json:"author,omitempty"`
	PublishedAt    string `json:"published_at"`
	SourceID       string `json:"source_id"`
	Rating         string `json:"rating,omitempty"`
	PreviousRating string `json:"previous_rating,omitempty"`
	RatingChange   string `json:"rating_change,omitempty"`
	URL            string `json:"url,omitempty"`
}
