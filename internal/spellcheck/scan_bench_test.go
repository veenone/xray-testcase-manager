package spellcheck

import (
	"fmt"
	"testing"
)

// BenchmarkScanTests measures a full scan over many tests with recurring typos,
// using a fresh checker each iteration (as ListMisspellings does per scan) so
// the per-scan cost, including the first-letter index build and a cold
// suggestion cache, is included.
func BenchmarkScanTests(b *testing.B) {
	typos := []string{"recieve", "occured", "seperate", "enviroment", "succesful", "acknowlege", "paramter", "reponse"}
	const nTests = 5000
	tests := make([]TestText, nTests)
	for i := range tests {
		w := typos[i%len(typos)]
		tests[i] = TestText{
			Key:         fmt.Sprintf("QA-%d", i),
			Summary:     "The system should " + w + " the request and return a valid response",
			Description: "Under normal operation the user can complete the flow without error and the data is persisted correctly",
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		c := NewDefaultChecker(nil)
		if got := ScanTests(tests, c); len(got) == 0 {
			b.Fatal("expected findings")
		}
	}
}
