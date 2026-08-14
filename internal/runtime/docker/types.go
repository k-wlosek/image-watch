package docker

// Minimal subsets of the Docker Engine API's JSON responses.

// containerSummary is one entry from GET /containers/json.
type containerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
}

// imageInspect is the relevant subset of GET /images/{id}/json.
type imageInspect struct {
	ID           string   `json:"Id"`
	RepoDigests  []string `json:"RepoDigests"`
	RepoTags     []string `json:"RepoTags"`
	Architecture string   `json:"Architecture"`
	Os           string   `json:"Os"`
	Variant      string   `json:"Variant"`
}
