package limiters

import "k8s.io/apimachinery/pkg/api/resource"

func adjustQuantity(resource, min, max *resource.Quantity) *resource.Quantity {
	switch {
	case resource.MilliValue() > max.MilliValue():
		resource = max
	case resource.MilliValue() < min.MilliValue():
		resource = min
	default:
	}
	return resource
}
