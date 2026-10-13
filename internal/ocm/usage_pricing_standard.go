package ocm

import (
	"regexp"
)

var (
	gpt52Pricing = ModelPricing{
		InputPrice:       1.75,
		OutputPrice:      14.0,
		CachedInputPrice: 0.175,
	}

	gpt5Pricing = ModelPricing{
		InputPrice:       1.25,
		OutputPrice:      10.0,
		CachedInputPrice: 0.125,
	}

	gpt5MiniPricing = ModelPricing{
		InputPrice:       0.25,
		OutputPrice:      2.0,
		CachedInputPrice: 0.025,
	}

	gpt5NanoPricing = ModelPricing{
		InputPrice:       0.05,
		OutputPrice:      0.4,
		CachedInputPrice: 0.005,
	}

	gpt52CodexPricing = ModelPricing{
		InputPrice:       1.75,
		OutputPrice:      14.0,
		CachedInputPrice: 0.175,
	}

	gpt51CodexPricing = ModelPricing{
		InputPrice:       1.25,
		OutputPrice:      10.0,
		CachedInputPrice: 0.125,
	}

	gpt51CodexMiniPricing = ModelPricing{
		InputPrice:       0.25,
		OutputPrice:      2.0,
		CachedInputPrice: 0.025,
	}

	gpt54StandardPricing = ModelPricing{
		InputPrice:       2.5,
		OutputPrice:      15.0,
		CachedInputPrice: 0.25,
	}

	gpt54PremiumPricing = ModelPricing{
		InputPrice:       5.0,
		OutputPrice:      22.5,
		CachedInputPrice: 0.5,
	}

	gpt54ProPricing = ModelPricing{
		InputPrice:       30.0,
		OutputPrice:      180.0,
		CachedInputPrice: 30.0,
	}

	gpt54ProPremiumPricing = ModelPricing{
		InputPrice:       60.0,
		OutputPrice:      270.0,
		CachedInputPrice: 60.0,
	}

	gpt52ProPricing = ModelPricing{
		InputPrice:       21.0,
		OutputPrice:      168.0,
		CachedInputPrice: 21.0,
	}

	gpt5ProPricing = ModelPricing{
		InputPrice:       15.0,
		OutputPrice:      120.0,
		CachedInputPrice: 15.0,
	}

	gpt4oPricing = ModelPricing{
		InputPrice:       2.5,
		OutputPrice:      10.0,
		CachedInputPrice: 1.25,
	}

	gpt4oMiniPricing = ModelPricing{
		InputPrice:       0.15,
		OutputPrice:      0.6,
		CachedInputPrice: 0.075,
	}

	gpt4oAudioPricing = ModelPricing{
		InputPrice:       2.5,
		OutputPrice:      10.0,
		CachedInputPrice: 2.5,
	}

	gpt4oMiniAudioPricing = ModelPricing{
		InputPrice:       0.15,
		OutputPrice:      0.6,
		CachedInputPrice: 0.15,
	}

	gptAudioMiniPricing = ModelPricing{
		InputPrice:       0.6,
		OutputPrice:      2.4,
		CachedInputPrice: 0.6,
	}

	o1Pricing = ModelPricing{
		InputPrice:       15.0,
		OutputPrice:      60.0,
		CachedInputPrice: 7.5,
	}

	o1ProPricing = ModelPricing{
		InputPrice:       150.0,
		OutputPrice:      600.0,
		CachedInputPrice: 150.0,
	}

	o1MiniPricing = ModelPricing{
		InputPrice:       1.1,
		OutputPrice:      4.4,
		CachedInputPrice: 0.55,
	}

	o3MiniPricing = ModelPricing{
		InputPrice:       1.1,
		OutputPrice:      4.4,
		CachedInputPrice: 0.55,
	}

	o3Pricing = ModelPricing{
		InputPrice:       2.0,
		OutputPrice:      8.0,
		CachedInputPrice: 0.5,
	}

	o3ProPricing = ModelPricing{
		InputPrice:       20.0,
		OutputPrice:      80.0,
		CachedInputPrice: 20.0,
	}

	o3DeepResearchPricing = ModelPricing{
		InputPrice:       10.0,
		OutputPrice:      40.0,
		CachedInputPrice: 2.5,
	}

	o4MiniPricing = ModelPricing{
		InputPrice:       1.1,
		OutputPrice:      4.4,
		CachedInputPrice: 0.275,
	}

	o4MiniDeepResearchPricing = ModelPricing{
		InputPrice:       2.0,
		OutputPrice:      8.0,
		CachedInputPrice: 0.5,
	}

	gpt41Pricing = ModelPricing{
		InputPrice:       2.0,
		OutputPrice:      8.0,
		CachedInputPrice: 0.5,
	}

	gpt41MiniPricing = ModelPricing{
		InputPrice:       0.4,
		OutputPrice:      1.6,
		CachedInputPrice: 0.1,
	}

	gpt41NanoPricing = ModelPricing{
		InputPrice:       0.1,
		OutputPrice:      0.4,
		CachedInputPrice: 0.025,
	}

	standardModelFamilies = []modelFamily{
		{
			pattern:        regexp.MustCompile(`^gpt-5\.4-pro(?:$|-)`),
			pricing:        gpt54ProPricing,
			premiumPricing: &gpt54ProPremiumPricing,
		},
		{
			pattern:        regexp.MustCompile(`^gpt-5\.4(?:$|-)`),
			pricing:        gpt54StandardPricing,
			premiumPricing: &gpt54PremiumPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.3-codex(?:$|-)`),
			pricing: gpt52CodexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.2-codex(?:$|-)`),
			pricing: gpt52CodexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1-codex-max(?:$|-)`),
			pricing: gpt51CodexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1-codex-mini(?:$|-)`),
			pricing: gpt51CodexMiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1-codex(?:$|-)`),
			pricing: gpt51CodexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-codex-mini(?:$|-)`),
			pricing: gpt51CodexMiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-codex(?:$|-)`),
			pricing: gpt51CodexPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.2-chat-latest$`),
			pricing: gpt52Pricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1-chat-latest$`),
			pricing: gpt5Pricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-chat-latest$`),
			pricing: gpt5Pricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.2-pro(?:$|-)`),
			pricing: gpt52ProPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-pro(?:$|-)`),
			pricing: gpt5ProPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-mini(?:$|-)`),
			pricing: gpt5MiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5-nano(?:$|-)`),
			pricing: gpt5NanoPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.2(?:$|-)`),
			pricing: gpt52Pricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5\.1(?:$|-)`),
			pricing: gpt5Pricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-5(?:$|-)`),
			pricing: gpt5Pricing,
		},
		{
			pattern: regexp.MustCompile(`^o4-mini-deep-research(?:$|-)`),
			pricing: o4MiniDeepResearchPricing,
		},
		{
			pattern: regexp.MustCompile(`^o4-mini(?:$|-)`),
			pricing: o4MiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^o3-pro(?:$|-)`),
			pricing: o3ProPricing,
		},
		{
			pattern: regexp.MustCompile(`^o3-deep-research(?:$|-)`),
			pricing: o3DeepResearchPricing,
		},
		{
			pattern: regexp.MustCompile(`^o3-mini(?:$|-)`),
			pricing: o3MiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^o3(?:$|-)`),
			pricing: o3Pricing,
		},
		{
			pattern: regexp.MustCompile(`^o1-pro(?:$|-)`),
			pricing: o1ProPricing,
		},
		{
			pattern: regexp.MustCompile(`^o1-mini(?:$|-)`),
			pricing: o1MiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^o1(?:$|-)`),
			pricing: o1Pricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4o-mini-audio(?:$|-)`),
			pricing: gpt4oMiniAudioPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-audio-mini(?:$|-)`),
			pricing: gptAudioMiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^(?:gpt-4o-audio|gpt-audio)(?:$|-)`),
			pricing: gpt4oAudioPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4\.1-nano(?:$|-)`),
			pricing: gpt41NanoPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4\.1-mini(?:$|-)`),
			pricing: gpt41MiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4\.1(?:$|-)`),
			pricing: gpt41Pricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4o-mini(?:$|-)`),
			pricing: gpt4oMiniPricing,
		},
		{
			pattern: regexp.MustCompile(`^gpt-4o(?:$|-)`),
			pricing: gpt4oPricing,
		},
		{
			pattern: regexp.MustCompile(`^chatgpt-4o(?:$|-)`),
			pricing: gpt4oPricing,
		},
	}
)
