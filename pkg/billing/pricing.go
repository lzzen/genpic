package billing

// PricingTable maps model IDs to a conservative per-request credit cost.
// This is the M0/M1 fixed-rate table; it will be replaced by a database-
// backed dynamic table in M4.
//
// Credits are the platform's internal unit. 1 credit ≈ the smallest billable
// unit the platform charges. The exchange rate to real money is not expressed
// here — only in the pricing configuration loaded at startup.
var PricingTable = map[string]CostEstimate{
	"openai/gpt-image-2":                    {Credits: 100, ModelID: "openai/gpt-image-2"},
	"gemini/gemini-2.5-flash-image":         {Credits: 30, ModelID: "gemini/gemini-2.5-flash-image"},
	"gemini/gemini-3.1-flash-image-preview": {Credits: 60, ModelID: "gemini/gemini-3.1-flash-image-preview"},
	"gemini/gemini-3-pro-image-preview":     {Credits: 120, ModelID: "gemini/gemini-3-pro-image-preview"},
	"wan/wan2.7-image":                      {Credits: 50, ModelID: "wan/wan2.7-image"},
	"wan/wan2.7-image-pro":                  {Credits: 100, ModelID: "wan/wan2.7-image-pro"},
}

// EstimateForModel returns the conservative credit estimate for the given
// model ID, scaling by the number of images requested.
// Returns a default estimate of 100 credits when the model is not in the table.
func EstimateForModel(modelID string, n int) CostEstimate {
	if n <= 0 {
		n = 1
	}
	base, ok := PricingTable[modelID]
	if !ok {
		return CostEstimate{Credits: int64(100 * n), ModelID: modelID}
	}
	return CostEstimate{Credits: base.Credits * int64(n), ModelID: modelID}
}
