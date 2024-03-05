package controllerstest

import (
	"fmt"
	"net/http"
	"time"

	"net/http/httptest"

	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
)

type fakeConfigGetter struct {
	fake *fakeclientset.Clientset
}

func (f *fakeConfigGetter) NewForConfig(_ *rest.Config) (kubernetes.Interface, error) {
	return f.fake, nil
}

type fakeHTTPClientGetter struct {
}

func (f *fakeHTTPClientGetter) ClientFor(_ http.RoundTripper, _ time.Duration) *http.Client {
	return restfake.CreateHTTPClient(func(*http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		fmt.Fprint(recorder, "ok")
		return recorder.Result(), nil
	})
}
