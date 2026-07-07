package coverage

import "xray-test-manager/internal/testrepo"

// GenerateTemplateWorkbook builds a blank, ready-to-fill parameter-extraction
// workbook whose sheet names and headers match exactly what ImportCoverageTemplate
// reads back — so a user can download it, fill in their function's parameters,
// and re-import it. A couple of example rows per sheet show the format.
//
// Keeping the template generated (rather than a bundled binary asset) means the
// download format and the import parser can never drift apart.
func GenerateTemplateWorkbook() ([]byte, error) {
	sheets := []testrepo.NamedRows{
		{
			Name:   "Parameter Values",
			Header: []string{"Parameter Group", "Parameter Value", "Type", "Description", "Linked Test(s)", "Status"},
			Rows: [][]string{
				{"Session", "Valid session handle", "CK_SESSION_HANDLE", "Obtained from C_OpenSession", "TEST-0001", ""},
				{"Session", "Read-only session handle", "CK_SESSION_HANDLE", "Session in read-only state", "", ""},
				{"Mechanism", "CKM_RSA_PKCS", "CKM_RSA_PKCS", "RSA with PKCS#1 v1.5", "TEST-0002, TEST-0003", ""},
				{"Mechanism", "CKM_ED25519", "CKM_ED25519", "EdDSA (Ed25519) signature", "", ""},
			},
		},
		{
			Name:   "Error Paths",
			Header: []string{"Error Code", "Description", "Trigger Condition", "Test Case(s)", "Status"},
			Rows: [][]string{
				{"CKR_OK", "Operation completed successfully", "All parameters valid", "TEST-0001", ""},
				{"CKR_ARGUMENTS_BAD", "Invalid argument (NULL pointer, etc.)", "pData NULL with ulDataLen > 0", "", ""},
			},
		},
		{
			Name:   "Boundary Conditions",
			Header: []string{"Parameter", "Boundary Type", "Value", "Expected Behavior", "Test Case", "Status"},
			Rows: [][]string{
				{"ulDataLen", "Minimum", "0", "Zero-length data allowed", "TEST-0001", ""},
				{"ulDataLen", "Just Above Max", "8388609 bytes", "Should fail: CKR_DATA_LEN_RANGE", "", ""},
			},
		},
		{
			Name:   "How to use",
			Header: []string{"Instructions"},
			Rows: [][]string{
				{"1. Fill the 'Parameter Values' sheet: one row per distinct value of each parameter, grouped by the Parameter Group column."},
				{"2. List the CKR_* return codes on the 'Error Paths' sheet (one row each)."},
				{"3. Optionally record edge cases on 'Boundary Conditions' (these are tracked but not counted toward the headline %)."},
				{"4. Put the test key(s) that exercise each value in the 'Linked Test(s)' / 'Test Case(s)' column (comma-separated)."},
				{"5. In the app: create a functional requirement on the Coverage tab, then 'Import template…' and pick this file."},
				{"Note: only test keys that exist in your synced project are mapped; the rest are reported as skipped."},
			},
		},
	}
	return testrepo.WriteXLSXSheets(sheets)
}
