package management

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

type usageCatalogItemV1 struct {
	ID            string `json:"id"`
	Label         string `json:"label,omitempty"`
	PriceKey      string `json:"price_key,omitempty"`
	Provider      string `json:"provider,omitempty"`
	ProviderState string `json:"provider_state,omitempty"`
}

type usageCatalogSourceV1 struct {
	SourceID      string `json:"source_id"`
	SourceKey     string `json:"source_key"`
	Label         string `json:"label,omitempty"`
	PriceKey      string `json:"price_key,omitempty"`
	Provider      string `json:"provider,omitempty"`
	ProviderState string `json:"provider_state,omitempty"`
}

type usageCatalogFactsV1 struct {
	SchemaVersion         int                    `json:"schema_version"`
	DatasetEpoch          string                 `json:"dataset_epoch"`
	Revision              uint64                 `json:"revision"`
	RewriteRevision       uint64                 `json:"rewrite_revision"`
	BillablePolicyVersion string                 `json:"billable_policy_version"`
	SourceKeyAlgorithm    string                 `json:"source_key_algorithm"`
	Models                []usageCatalogItemV1   `json:"models"`
	PriceKeys             []usageCatalogItemV1   `json:"price_keys"`
	Sources               []usageCatalogSourceV1 `json:"sources"`
}

type usageCatalogResponseV1 struct {
	usageCatalogFactsV1
	GeneratedAt time.Time `json:"generated_at"`
}

// GetUsageCatalog returns the all-time projection catalog without copying
// canonical request details.
func (h *Handler) GetUsageCatalog(c *gin.Context) {
	setUsageNoStore(c)
	if h == nil || h.usageStats == nil {
		writeUsageProjectionError(c, usage.ErrProjectionUnavailable)
		return
	}
	snapshot, err := h.usageStats.QueryProjectionCatalog()
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	facts := buildUsageCatalogFactsV1(snapshot)
	etagFacts := facts
	etagFacts.Revision = 0
	etagFacts.RewriteRevision = 0
	etag, err := usageRepresentationETag("catalog-v1", etagFacts)
	if err != nil {
		writeUsageProjectionError(c, err)
		return
	}
	setUsageConditionalHeaders(c, etag, snapshot.Revision)
	if usageIfNoneMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, usageCatalogResponseV1{
		usageCatalogFactsV1: facts,
		GeneratedAt:         h.usageCurrentTime(),
	})
}

func buildUsageCatalogFactsV1(snapshot usage.ProjectionSnapshot) usageCatalogFactsV1 {
	result := usageCatalogFactsV1{
		SchemaVersion:         1,
		DatasetEpoch:          usageDatasetEpochID(snapshot.DatasetEpoch),
		Revision:              snapshot.Revision,
		RewriteRevision:       snapshot.RewriteRevision,
		BillablePolicyVersion: usage.BillablePolicyVersionV1,
		SourceKeyAlgorithm:    usage.UsageSourceKeyVersionV1,
		Models:                make([]usageCatalogItemV1, 0, len(snapshot.Catalog.Models)),
		PriceKeys:             make([]usageCatalogItemV1, 0, len(snapshot.Catalog.PriceKeys)),
		Sources:               make([]usageCatalogSourceV1, 0, len(snapshot.Catalog.Sources)),
	}
	for _, entry := range snapshot.Catalog.Models {
		result.Models = append(result.Models, usageCatalogItemV1{
			ID: entry.ID, Label: entry.Label, PriceKey: entry.PriceKey,
			Provider: entry.Provider, ProviderState: entry.ProviderState,
		})
	}
	for _, entry := range snapshot.Catalog.PriceKeys {
		result.PriceKeys = append(result.PriceKeys, usageCatalogItemV1{
			ID: entry.ID, Label: entry.Label, PriceKey: entry.PriceKey,
			Provider: entry.Provider, ProviderState: entry.ProviderState,
		})
	}
	for sourceID, entry := range snapshot.Catalog.Sources {
		result.Sources = append(result.Sources, usageCatalogSourceV1{
			SourceID: sourceID, SourceKey: entry.Label, Label: entry.Label,
			PriceKey: entry.PriceKey, Provider: entry.Provider, ProviderState: entry.ProviderState,
		})
	}
	sort.Slice(result.Models, func(i, j int) bool { return result.Models[i].ID < result.Models[j].ID })
	sort.Slice(result.PriceKeys, func(i, j int) bool { return result.PriceKeys[i].ID < result.PriceKeys[j].ID })
	sort.Slice(result.Sources, func(i, j int) bool { return result.Sources[i].SourceID < result.Sources[j].SourceID })
	return result
}

func (h *Handler) usageCurrentTime() time.Time {
	if h != nil && h.usageNow != nil {
		return h.usageNow().UTC()
	}
	return time.Now().UTC()
}

func usageDatasetEpochID(epoch uint64) string {
	return "epoch:v1:" + strconv.FormatUint(epoch, 36)
}

func setUsageNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}

func setUsageConditionalHeaders(c *gin.Context, etag string, revision uint64) {
	c.Header("ETag", etag)
	c.Header("X-Usage-Revision", strconv.FormatUint(revision, 10))
}

func usageRepresentationETag(kind string, facts any) (string, error) {
	payload, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(kind))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(payload)
	return `W/"` + hex.EncodeToString(hash.Sum(nil)) + `"`, nil
}

func usageIfNoneMatch(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

func writeUsageProjectionError(c *gin.Context, err error) {
	c.Header("Retry-After", "1")
	payload := gin.H{
		"error":            "projection_unavailable",
		"code":             "projection_unavailable",
		"projection_state": "unavailable",
	}
	var budgetErr *usage.ProjectionBudgetError
	if errors.As(err, &budgetErr) && budgetErr != nil && strings.TrimSpace(budgetErr.Dimension) != "" {
		payload["projection_state"] = "budget_exceeded"
		payload["budget_dimension"] = budgetErr.Dimension
	}
	c.JSON(http.StatusServiceUnavailable, payload)
}
