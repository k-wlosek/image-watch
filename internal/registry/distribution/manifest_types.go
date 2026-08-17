package distribution

// Media types understood by the registry client.
const (
	MediaTypeOCIManifest        = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIIndex           = "application/vnd.oci.image.index.v1+json"
	MediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// Accept is the Accept header sent on manifest/index requests.
var Accept = []string{
	MediaTypeOCIIndex,
	MediaTypeOCIManifest,
	MediaTypeDockerManifestList,
	MediaTypeDockerManifest,
}

// manifestEnvelope is used to sniff mediaType before decoding.
type manifestEnvelope struct {
	MediaType     string `json:"mediaType"`
	SchemaVersion int    `json:"schemaVersion"`
}

// ociPlatform mirrors the platform object embedded in index entries.
type ociPlatform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// indexEntry is one entry in an image index or manifest list.
type indexEntry struct {
	MediaType string      `json:"mediaType"`
	Digest    string      `json:"digest"`
	Platform  ociPlatform `json:"platform"`
}

// imageIndex is the top-level image index structure.
type imageIndex struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Manifests     []indexEntry `json:"manifests"`
}

// tagListResponse is the body of GET /v2/<repository>/tags/list.
type tagListResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// manifestConfigRef captures just the "config" descriptor present on a
// single-platform manifest.
type manifestConfigRef struct {
	Config struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"config"`
}

// imageConfig is the minimal subset of the OCI/Docker image config JSON
// needed to determine which platform a single-platform manifest targets.
type imageConfig struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

func isIndexMediaType(mt string) bool {
	return mt == MediaTypeOCIIndex || mt == MediaTypeDockerManifestList
}
