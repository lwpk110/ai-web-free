package promptplaza

import (
	_ "embed"
	"encoding/json"
	"strings"
	"time"
)

//go:embed seeds.json
var embeddedSeedsJSON []byte

var seedPreviewURLs = map[string]string{
	"seed-photo-glass-greenhouse":    "https://images.unsplash.com/photo-1518531933037-91b2f5f229cc?auto=format&fit=crop&w=640&q=80",
	"seed-photo-polaroid-peel":       "https://images.unsplash.com/photo-1526170375885-4d8ecf77b99f?auto=format&fit=crop&w=640&q=80",
	"seed-photo-nordic-interior":     "https://images.unsplash.com/photo-1586023492125-27b2c045efd7?auto=format&fit=crop&w=640&q=80",
	"seed-photo-macro-raindrop":      "https://images.unsplash.com/photo-1501004318641-b39e6451bec6?auto=format&fit=crop&w=640&q=80",
	"seed-photo-tokyo-rain-night":    "https://images.unsplash.com/photo-1503899036084-c55cdd92da26?auto=format&fit=crop&w=640&q=80",
	"seed-3d-glass-capsule":          "https://images.unsplash.com/photo-1509198397868-475647b2a1e5?auto=format&fit=crop&w=640&q=80",
	"seed-3d-artisan-keycap":         "https://images.unsplash.com/photo-1587829741301-dc798b83add3?auto=format&fit=crop&w=640&q=80",
	"seed-3d-blindbox-toy":           "https://images.unsplash.com/photo-1566576912321-d58ddd7a6088?auto=format&fit=crop&w=640&q=80",
	"seed-3d-isometric-city":         "https://images.unsplash.com/photo-1518709268805-4e9042af9f23?auto=format&fit=crop&w=640&q=80",
	"seed-3d-knolling-kit":           "https://images.unsplash.com/photo-1492691527719-9d1e07e534b4?auto=format&fit=crop&w=640&q=80",
	"seed-poster-swiss-typo":         "https://images.unsplash.com/photo-1561070791-2526d30994b5?auto=format&fit=crop&w=640&q=80",
	"seed-poster-retro-scifi":        "https://images.unsplash.com/photo-1451187580459-43490279c0fa?auto=format&fit=crop&w=640&q=80",
	"seed-poster-coffee-menu":        "https://images.unsplash.com/photo-1495474472287-4d71bcdd2085?auto=format&fit=crop&w=640&q=80",
	"seed-poster-neon-festival":      "https://images.unsplash.com/photo-1550684848-fac1c5b4e853?auto=format&fit=crop&w=640&q=80",
	"seed-illus-kyoto-watercolor":    "https://images.unsplash.com/photo-1493976040374-85c8e12f0c0e?auto=format&fit=crop&w=640&q=80",
	"seed-illus-lofi-study":          "https://images.unsplash.com/photo-1519681393784-d120267933ba?auto=format&fit=crop&w=640&q=80",
	"seed-illus-ukiyoe-cyber":        "https://images.unsplash.com/photo-1578301978693-85fa9c0320b9?auto=format&fit=crop&w=640&q=80",
	"seed-illus-storybook-gouache":   "https://images.unsplash.com/photo-1513364776144-60967b0f800f?auto=format&fit=crop&w=640&q=80",
	"seed-product-titanium-synth":    "https://images.unsplash.com/photo-1511671782779-c97d3d27a1d4?auto=format&fit=crop&w=640&q=80",
	"seed-product-frosted-glass-ui":  "https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?auto=format&fit=crop&w=640&q=80",
	"seed-product-perfume-bottle":    "https://images.unsplash.com/photo-1541643600914-78b084683601?auto=format&fit=crop&w=640&q=80",
	"seed-product-mobile-dashboard":  "https://images.unsplash.com/photo-1551288049-bebda4e38f71?auto=format&fit=crop&w=640&q=80",
	"seed-edit-material-remodel":     "https://images.unsplash.com/photo-1634017839464-5c339ebe3cb4?auto=format&fit=crop&w=640&q=80",
	"seed-edit-ghibli-style":         "https://images.unsplash.com/photo-1506744038136-46273834b3fb?auto=format&fit=crop&w=640&q=80",
	"seed-edit-product-scene":        "https://images.unsplash.com/photo-1523275335684-37898b6baf30?auto=format&fit=crop&w=640&q=80",
	"seed-edit-3d-figurine":          "https://images.unsplash.com/photo-1534447677768-be436bb09401?auto=format&fit=crop&w=640&q=80",
}

var categoryPreviewFallbacks = map[string][]string{
	CategoryPhotography: {
		seedPreviewURLs["seed-photo-glass-greenhouse"],
		seedPreviewURLs["seed-photo-tokyo-rain-night"],
		seedPreviewURLs["seed-photo-nordic-interior"],
	},
	Category3DMiniature: {
		seedPreviewURLs["seed-3d-glass-capsule"],
		seedPreviewURLs["seed-3d-isometric-city"],
		seedPreviewURLs["seed-3d-artisan-keycap"],
	},
	CategoryPosterTypography: {
		seedPreviewURLs["seed-poster-swiss-typo"],
		seedPreviewURLs["seed-poster-neon-festival"],
		seedPreviewURLs["seed-poster-retro-scifi"],
	},
	CategoryIllustrationAnime: {
		seedPreviewURLs["seed-illus-kyoto-watercolor"],
		seedPreviewURLs["seed-illus-lofi-study"],
		seedPreviewURLs["seed-illus-ukiyoe-cyber"],
	},
	CategoryProductUI: {
		seedPreviewURLs["seed-product-frosted-glass-ui"],
		seedPreviewURLs["seed-product-titanium-synth"],
		seedPreviewURLs["seed-product-perfume-bottle"],
	},
	CategoryReferenceEdit: {
		seedPreviewURLs["seed-edit-material-remodel"],
		seedPreviewURLs["seed-edit-ghibli-style"],
		seedPreviewURLs["seed-edit-product-scene"],
	},
}

func defaultCategoryPreviewURL(category, id string) string {
	pool := categoryPreviewFallbacks[category]
	if len(pool) == 0 {
		pool = categoryPreviewFallbacks[CategoryPhotography]
	}
	var sum int
	for _, r := range id {
		sum += int(r)
	}
	return pool[sum%len(pool)]
}

// LoadSeedItems 加载编译期内置的高分精选提示词种子库。
func LoadSeedItems() ([]PromptItem, error) {
	var items []PromptItem
	if err := json.Unmarshal(embeddedSeedsJSON, &items); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	out := make([]PromptItem, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.PreviewURL) == "" {
			it.PreviewURL = seedPreviewURLs[it.ID]
		}
		norm := normalizePromptItem(it, now)
		if norm.ID == "" || norm.PromptEN == "" {
			continue
		}
		if norm.Source == "" {
			norm.Source = SourceSeed
		}
		out = append(out, norm)
	}
	return out, nil
}
