package spellcheck

// domainAllowList holds recurring domain vocabulary that a general English
// dictionary flags but which is correct in this product's test content:
// product/spec names, standards acronyms rendered in lowercase, and Gherkin
// keywords. Add terms here (lowercase) as they surface; user-specific terms
// belong in the per-user ignore list instead.
var domainAllowList = []string{
	// product & spec names
	"xray", "euicc", "esim", "pkcs", "cryptoki", "aspice",
	// standards / telecom acronyms (lowercased forms)
	"rsp", "mno", "gsma", "sgp", "isd", "kiwi", "tcms",
	// Gherkin keywords
	"given", "when", "then", "scenario", "feature", "background", "outline", "examples",
}
