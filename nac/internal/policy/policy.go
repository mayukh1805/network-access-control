package policy

type Decision string

const (
	Allow Decision = "ALLOW"
	Deny  Decision = "DENY"
)

func Evaluate(role, resource, action string) Decision {
	if role == "Guest" && resource == "internet" && action == "access" {
		return Allow
	}

	if role == "Guest" && resource == "printer" && action == "print" {
		return Deny
	}

	if role == "Employee" && resource == "internet" && action == "access" {
		return Allow
	}

	if role == "Employee" && resource == "printer" && action == "print" {
		return Allow
	}

	if role == "Maintenance" && resource == "printer" && action == "print" {
		return Allow
	}

	if role == "Administrator" && resource == "management" && action == "access" {
		return Allow
	}

	return Deny
}
