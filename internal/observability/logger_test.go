package observability

import (
	"strings"
	"testing"
)

func TestRedactString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Redact phone number",
			input:    "Pesan dari 628123456789 masuk",
			expected: "Pesan dari 6281***789 masuk",
		},
		{
			name:     "Redact URL credentials",
			input:    "Koneksi ke postgresql://postgres:mypassword123@db.supabase.co:5432/postgres",
			expected: "Koneksi ke postgresql://[REDACTED]@db.supabase.co:5432/postgres",
		},
		{
			name:     "Safe string untouched",
			input:    "Halo apa kabar dunia",
			expected: "Halo apa kabar dunia",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactString(tt.input)
			if !strings.Contains(got, "[REDACTED]") && strings.Contains(tt.input, "@") {
				t.Errorf("Expected URL redaction, got %s", got)
			}
			if strings.Contains(tt.input, "628123456789") && strings.Contains(got, "628123456789") {
				t.Errorf("Expected phone redaction, got %s", got)
			}
		})
	}
}
