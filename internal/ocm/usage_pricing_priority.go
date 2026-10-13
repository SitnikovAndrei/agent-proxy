package ocm

import (
	"regexp"
)

var (
	gpt54PriorityPricing = ModelPricing{
		InputPrice:       5.0,
		OutputPrice:      30.0,
		CachedInputPrice: 0.5,
	}

	gpt54PremiumPriorityPricing = ModelPricing{
		InputPrice:       10.0,
		OutputPrice:      45.0,
		CachedInputPrice: 1.0,
	}

	gpt52PriorityPricing = ModelPricing{
		InputPrice:       3.5,
		OutputPrice:      28.0,
		CachedInputPrice: 0.35,
	}

	gpt5PriorityPricing = ModelPricing{
		InputPrice:       2.5,
		OutputPrice:      20.0,
		CachedInputPrice: 0.25,
	}

	gpt5MiniPriorityPricing = ModelPricing{
		InputPrice:       0.45,
		OutputPrice:      3.6,
		CachedInputPrice: 0.045,
	}

	gpt52CodexPriorityPricing = ModelPricing{
		InputPrice:       3.5,
		OutputPrice:      28.0,
		CachedInputPrice: 0.35,
	}

	gpt51CodexPriorityPricing = ModelPricing{
		InputPrice:       2.5,
		OutputPrice:      20.0,
		CachedInputPrice: 0.25,
	}

	o3PriorityPricing = ModelPricing{
		InputPrice:       3.5,
		OutputPrice:      14.0,
		CachedInputPrice: 0.875,
	}

	o4MiniPriorityPricing = ModelPricing{
		InputPrice:       2.0,
		OutputPrice:      8.0,
		CachedInputPrice: 0.5,
	}

	gpt41PriorityPricing = ModelPricing{
		InputPrice:       3.5,
		OutputPrice:      14.0,
		CachedInputPrice: 0.875,
	}

	gpt41MiniPriorityPricing = ModelPricing{
		InputPrice:       0.7,
		OutputPrice:      2.8,
		CachedInputPrice: 0.175,
	}

	gpt41NanoPriorityPricing = ModelPricing{
		InputPrice:       0.2,
		OutputPrice:      0.8,
		CachedInputPrice: 0.05,
	}

	gpt4oPriorityPricing = ModelPricing{
		InputPrice:       4.25,
		OutputPrice:      17.0,
		CachedInputPrice: 2.125,
	}

	gpt4oMiniPriorityPricing = ModelPricing{
		InputPrice:       0.25,
		OutputPrice:      1.0,
		CachedInputPrice: 0.125,
	}

	priorityModelFamilies = []modelFamily{
		{
			pattern:        regexp.MustCompile(`^gpt-5\.4(?:$|-)`),
			pricing:        gpt54PriorityPricing,
			premiumPricing: &gpt54PremiumPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.3-codex(?:$|-)`),
			pricing: gpt52CodexPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.2-codex(?:$|-)`),
			pricing: gpt52CodexPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1-codex-max(?:$|-)`),
			pricing: gpt51CodexPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1-codex(?:$|-)`),
			pricing: gpt51CodexPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-codex-mini(?:$|-)`),
			pricing: gpt5MiniPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-codex(?:$|-)`),
			pricing: gpt51CodexPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-mini(?:$|-)`),
			pricing: gpt5MiniPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.2(?:$|-)`),
			pricing: gpt52PriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1(?:$|-)`),
			pricing: gpt5PriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5(?:$|-)`),
			pricing: gpt5PriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^o4-mini(?:$|-)`),
			pricing: o4MiniPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^o3(?:$|-)`),
			pricing: o3PriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4\.1-nano(?:$|-)`),
			pricing: gpt41NanoPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4\.1-mini(?:$|-)`),
			pricing: gpt41MiniPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4\.1(?:$|-)`),
			pricing: gpt41PriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4o-mini(?:$|-)`),
			pricing: gpt4oMiniPriorityPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4o(?:$|-)`),
			pricing: gpt4oPriorityPricing,
		},
	}
)
