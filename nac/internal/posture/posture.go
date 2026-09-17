package posture

type Status string

const (
	Healthy   Status = "HEALTHY"
	Unhealthy Status = "UNHEALTHY"
)

func CheckDeviceStatus(status string) Status {
	if status == "active" {
		return Healthy
	}

	return Unhealthy
}
