package policy

import "testing"

func TestEvaluate(t *testing.T) {
	tests := []struct {
		role     string
		resource string
		action   string
		expected Decision
	}{
		{"Guest", "internet", "access", Allow},
		{"Guest", "printer", "print", Deny},
		{"Employee", "internet", "access", Allow},
		{"Employee", "printer", "print", Allow},
		{"Maintenance", "printer", "print", Allow},
		{"Administrator", "management", "access", Allow},
		{"Guest", "management", "access", Deny},
	}

	for _, tt := range tests {
		got := Evaluate(tt.role, tt.resource, tt.action)

		if got != tt.expected {
			t.Errorf(
				"Evaluate(%q, %q, %q) = %q; want %q",
				tt.role, tt.resource, tt.action, got, tt.expected,
			)
		}
	}
}
