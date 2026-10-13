package ocm

import (
	"regexp"
)

var (
	gpt54FlexPricing = ModelPricing{
		InputPrice:       1.25,
		OutputPrice:      7.5,
		CachedInputPrice: 0.125,
	}

	gpt54PremiumFlexPricing = ModelPricing{
		InputPrice:       2.5,
		OutputPrice:      11.25,
		CachedInputPrice: 0.25,
	}

	gpt54ProFlexPricing = ModelPricing{
		InputPrice:       15.0,
		OutputPrice:      90.0,
		CachedInputPrice: 15.0,
	}

	gpt54ProPremiumFlexPricing = ModelPricing{
		InputPrice:       30.0,
		OutputPrice:      135.0,
		CachedInputPrice: 30.0,
	}

	gpt52FlexPricing = ModelPricing{
		InputPrice:       0.875,
		OutputPrice:      7.0,
		CachedInputPrice: 0.0875,
	}

	gpt5FlexPricing = ModelPricing{
		InputPrice:       0.625,
		OutputPrice:      5.0,
		CachedInputPrice: 0.0625,
	}

	gpt5MiniFlexPricing = ModelPricing{
		InputPrice:       0.125,
		OutputPrice:      1.0,
		CachedInputPrice: 0.0125,
	}

	gpt5NanoFlexPricing = ModelPricing{
		InputPrice:       0.025,
		OutputPrice:      0.2,
		CachedInputPrice: 0.0025,
	}

	o3FlexPricing = ModelPricing{
		InputPrice:       1.0,
		OutputPrice:      4.0,
		CachedInputPrice: 0.25,
	}

	o4MiniFlexPricing = ModelPricing{
		InputPrice:       0.55,
		OutputPrice:      2.2,
		CachedInputPrice: 0.138,
	}

	flexModelFamilies = []modelFamily{
		{
			pattern:        regexp.MustCompile(`^gpt-5\.4-pro(?:$|-)`),
			pricing:        gpt54ProFlexPricing,
			premiumPricing: &gpt54ProPremiumFlexPricing,
		},
		{
			pattern:        regexp.MustCompile(`^gpt-5\.4(?:$|-)`),
			pricing:        gpt54FlexPricing,
			premiumPricing: &gpt54PremiumFlexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-mini(?:$|-)`),
			pricing: gpt5MiniFlexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-nano(?:$|-)`),
			pricing: gpt5NanoFlexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.2(?:$|-)`),
			pricing: gpt52FlexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1(?:$|-)`),
			pricing: gpt5FlexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5(?:$|-)`),
			pricing: gpt5FlexPricing,
		},
		{
			pattern: regexp.MustCompile(`^o4-mini(?:$|-)`),
			pricing: o4MiniFlexPricing,
		},
		{
			pattern: regexp.MustCompile(`^o3(?:$|-)`),
			pricing: o3FlexPricing,
		},
	}
)
