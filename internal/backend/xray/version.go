package xray

import "time"

// jiraTimeFormats lists the timestamp layouts Jira/Xray is known to return
// for issue `updated` values — typically "yyyy-MM-ddTHH:mm:ss.SSS-HHMM" but
// RFC 3339 variants are also accepted.
var jiraTimeFormats = []string{
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05.000Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
}

// parseJiraTime parses s against each of jiraTimeFormats in turn, returning
// the first successful match.
func parseJiraTime(s string) (time.Time, bool) {
	for _, f := range jiraTimeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
