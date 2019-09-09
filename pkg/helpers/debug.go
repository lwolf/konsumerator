package helpers

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

func PrettyPrintResources(r *corev1.ResourceRequirements) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf(
		"Req: cpu:%s, ram:%s; Limit: cpu:%s, ram:%s",
		r.Requests.Cpu().String(),
		r.Requests.Memory().String(),
		r.Limits.Cpu().String(),
		r.Limits.Memory().String(),
	)
}
