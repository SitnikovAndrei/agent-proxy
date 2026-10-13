package ccm

import (
	"regexp"
)

var (
	opus46StandardPricing = ModelPricing{
		InputPrice:             5.0,
		OutputPrice:            25.0,
		CacheReadPrice:         0.5,
		CacheWritePrice5Minute: 6.25,
		CacheWritePrice1Hour:   10.0,
	}

	opus46PremiumPricing = ModelPricing{
		InputPrice:             10.0,
		OutputPrice:            37.5,
		CacheReadPrice:         1.0,
		CacheWritePrice5Minute: 12.5,
		CacheWritePrice1Hour:   20.0,
	}

	opus45Pricing = ModelPricing{
		InputPrice:             5.0,
		OutputPrice:            25.0,
		CacheReadPrice:         0.5,
		CacheWritePrice5Minute: 6.25,
		CacheWritePrice1Hour:   10.0,
	}

	opus4Pricing = ModelPricing{
		InputPrice:             15.0,
		OutputPrice:            75.0,
		CacheReadPrice:         1.5,
		CacheWritePrice5Minute: 18.75,
		CacheWritePrice1Hour:   30.0,
	}

	sonnet46StandardPricing = ModelPricing{
		InputPrice:             3.0,
		OutputPrice:            15.0,
		CacheReadPrice:         0.3,
		CacheWritePrice5Minute: 3.75,
		CacheWritePrice1Hour:   6.0,
	}

	sonnet46PremiumPricing = ModelPricing{
		InputPrice:             6.0,
		OutputPrice:            22.5,
		CacheReadPrice:         0.6,
		CacheWritePrice5Minute: 7.5,
		CacheWritePrice1Hour:   12.0,
	}

	sonnet45StandardPricing = ModelPricing{
		InputPrice:             3.0,
		OutputPrice:            15.0,
		CacheReadPrice:         0.3,
		CacheWritePrice5Minute: 3.75,
		CacheWritePrice1Hour:   6.0,
	}

	sonnet45PremiumPricing = ModelPricing{
		InputPrice:             6.0,
		OutputPrice:            22.5,
		CacheReadPrice:         0.6,
		CacheWritePrice5Minute: 7.5,
		CacheWritePrice1Hour:   12.0,
	}

	sonnet4StandardPricing = ModelPricing{
		InputPrice:             3.0,
		OutputPrice:            15.0,
		CacheReadPrice:         0.3,
		CacheWritePrice5Minute: 3.75,
		CacheWritePrice1Hour:   6.0,
	}

	sonnet4PremiumPricing = ModelPricing{
		InputPrice:             6.0,
		OutputPrice:            22.5,
		CacheReadPrice:         0.6,
		CacheWritePrice5Minute: 7.5,
		CacheWritePrice1Hour:   12.0,
	}

	sonnet37Pricing = ModelPricing{
		InputPrice:             3.0,
		OutputPrice:            15.0,
		CacheReadPrice:         0.3,
		CacheWritePrice5Minute: 3.75,
		CacheWritePrice1Hour:   6.0,
	}

	sonnet35Pricing = ModelPricing{
		InputPrice:             3.0,
		OutputPrice:            15.0,
		CacheReadPrice:         0.3,
		CacheWritePrice5Minute: 3.75,
		CacheWritePrice1Hour:   6.0,
	}

	haiku45Pricing = ModelPricing{
		InputPrice:             1.0,
		OutputPrice:            5.0,
		CacheReadPrice:         0.1,
		CacheWritePrice5Minute: 1.25,
		CacheWritePrice1Hour:   2.0,
	}

	haiku4Pricing = ModelPricing{
		InputPrice:             1.0,
		OutputPrice:            5.0,
		CacheReadPrice:         0.1,
		CacheWritePrice5Minute: 1.25,
		CacheWritePrice1Hour:   2.0,
	}

	haiku35Pricing = ModelPricing{
		InputPrice:             0.8,
		OutputPrice:            4.0,
		CacheReadPrice:         0.08,
		CacheWritePrice5Minute: 1.0,
		CacheWritePrice1Hour:   1.6,
	}

	haiku3Pricing = ModelPricing{
		InputPrice:             0.25,
		OutputPrice:            1.25,
		CacheReadPrice:         0.03,
		CacheWritePrice5Minute: 0.3,
		CacheWritePrice1Hour:   0.5,
	}

	opus3Pricing = ModelPricing{
		InputPrice:             15.0,
		OutputPrice:            75.0,
		CacheReadPrice:         1.5,
		CacheWritePrice5Minute: 18.75,
		CacheWritePrice1Hour:   30.0,
	}

	modelFamilies = []modelFamily{
		{
			pattern:         regexp.MustCompile(`^claude-opus-4-6(?:-|$)`),
			standardPricing: opus46StandardPricing,
			premiumPricing:  &opus46PremiumPricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-opus-4-5(?:-|$)`),
			standardPricing: opus45Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-(?:opus-4(?:-|$)|4-opus-)`),
			standardPricing: opus4Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-(?:opus-3(?:-|$)|3-opus-)`),
			standardPricing: opus3Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-(?:sonnet-4-6(?:-|$)|4-6-sonnet-)`),
			standardPricing: sonnet46StandardPricing,
			premiumPricing:  &sonnet46PremiumPricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-(?:sonnet-4-5(?:-|$)|4-5-sonnet-)`),
			standardPricing: sonnet45StandardPricing,
			premiumPricing:  &sonnet45PremiumPricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-(?:sonnet-4(?:-|$)|4-sonnet-)`),
			standardPricing: sonnet4StandardPricing,
			premiumPricing:  &sonnet4PremiumPricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-3-7-sonnet(?:-|$)`),
			standardPricing: sonnet37Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-3-5-sonnet(?:-|$)`),
			standardPricing: sonnet35Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-(?:haiku-4-5(?:-|$)|4-5-haiku-)`),
			standardPricing: haiku45Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-haiku-4(?:-|$)`),
			standardPricing: haiku4Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-3-5-haiku(?:-|$)`),
			standardPricing: haiku35Pricing,
		},
		{
			pattern:         regexp.MustCompile(`^claude-3-haiku(?:-|$)`),
			standardPricing: haiku3Pricing,
		},
	}
)
