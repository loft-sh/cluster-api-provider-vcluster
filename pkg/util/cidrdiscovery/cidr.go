package cidrdiscovery

import (
	"context"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errorMessageFind = "The range of valid IPs is "

func NewCIDRLookup(c client.Client) *serviceCIDR {
	return &serviceCIDR{
		client: c,
	}
}

type serviceCIDR struct {
	client           client.Client
	serviceCIDRMutex sync.Mutex
	serviceCIDR      string
}

func (s *serviceCIDR) GetServiceCIDR(ctx context.Context, namespace string) (string, error) {
	s.serviceCIDRMutex.Lock()
	defer s.serviceCIDRMutex.Unlock()
	if s.serviceCIDR != "" {
		return s.serviceCIDR, nil
	}

	// find out server cidr
	err := s.client.Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-service-",
			Namespace:    namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
			ClusterIP: "4.4.4.4",
		},
	})
	if err == nil {
		return "", fmt.Errorf("couldn't find cluster service cidr")
	} else {
		errorMessage := err.Error()
		idx := strings.Index(errorMessage, errorMessageFind)
		if idx == -1 {
			return "", fmt.Errorf("couldn't find cluster service cidr (" + errorMessage + ")")
		} else {
			s.serviceCIDR = strings.TrimSpace(errorMessage[idx+len(errorMessageFind):])
		}
	}
	return s.serviceCIDR, nil
}
