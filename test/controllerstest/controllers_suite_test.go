package controllerstest

import (
	"context"
	"testing"
	"time"

	"github.com/loft-sh/cluster-api-provider-vcluster/api/v1alpha1"
	"github.com/loft-sh/cluster-api-provider-vcluster/controllers"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	fakeclientset "k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	kubeconfigBytes = []byte(`
kind: Config
apiVersion: v1
clusters:
- cluster:
    api-version: v1
    server: https://test:443
    certificate-authority: test.crt
  name: kubeconfig-cluster
users:
- name: kubeconfig-user
  user:
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJrakNDQVRlZ0F3SUJBZ0lJT2FQRzhMc21MNWd3Q2dZSUtvWkl6ajBFQXdJd0l6RWhNQjhHQTFVRUF3d1kKYXpOekxXTnNhV1Z1ZEMxallVQXhOekE0TURBNE1qRXpNQjRYRFRJME1ESXhOVEUwTkRNek0xb1hEVEkxTURJeApOREUwTkRNek0xb3dNREVYTUJVR0ExVUVDaE1PYzNsemRHVnRPbTFoYzNSbGNuTXhGVEFUQmdOVkJBTVRESE41CmMzUmxiVHBoWkcxcGJqQlpNQk1HQnlxR1NNNDlBZ0VHQ0NxR1NNNDlBd0VIQTBJQUJDbysyRzRzQ0pjaTVZTlMKMkp6VTd5ZnEzSUR0dE1tcnU2bGtGV2NMR2FJSVRTVDZPbFdzaDdaYkJRb3FrTkk5c3dTOStCWHptV2FOQ1FzRgp1Q0ZaL0F1alNEQkdNQTRHQTFVZER3RUIvd1FFQXdJRm9EQVRCZ05WSFNVRUREQUtCZ2dyQmdFRkJRY0RBakFmCkJnTlZIU01FR0RBV2dCUyt0MG1hMFR2ZHN5d2RuVGpYd0ExWis0eFZJakFLQmdncWhrak9QUVFEQWdOSkFEQkcKQWlFQThjZXNlcWhjOFpGU0Z3TERzdDJYUS9lU0xiVWFuNnNYenhFeHFtSlNEbXNDSVFEcDdJWmRJd3FaVmY2WQpQMWRaOWwzeE9JTDFRL2Y5VXdNVC9aOFRaZEZJa2c9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCi0tLS0tQkVHSU4gQ0VSVElGSUNBVEUtLS0tLQpNSUlCZGpDQ0FSMmdBd0lCQWdJQkFEQUtCZ2dxaGtqT1BRUURBakFqTVNFd0h3WURWUVFEREJock0zTXRZMnhwClpXNTBMV05oUURFM01EZ3dNRGd5TVRNd0hoY05NalF3TWpFMU1UUTBNek16V2hjTk16UXdNakV5TVRRME16TXoKV2pBak1TRXdId1lEVlFRRERCaHJNM010WTJ4cFpXNTBMV05oUURFM01EZ3dNRGd5TVRNd1dUQVRCZ2NxaGtqTwpQUUlCQmdncWhrak9QUU1CQndOQ0FBVCtZbTVnL0o4TzIwQ0llSFB4Z2hRWTBXajl3QVZzc0QxdHRzS0VnMFFRCjA3UDNLZEttV3AzS3BvV3FkdkN4dTNFMkp4ZDBGVDh5eG1IOVJiamVXRW90bzBJd1FEQU9CZ05WSFE4QkFmOEUKQkFNQ0FxUXdEd1lEVlIwVEFRSC9CQVV3QXdFQi96QWRCZ05WSFE0RUZnUVV2cmRKbXRFNzNiTXNIWjA0MThBTgpXZnVNVlNJd0NnWUlLb1pJemowRUF3SURSd0F3UkFJZ1VldS9yVnBmc1NoUUZmSjIyb05CMVhwY1djUWFPY2FBCnF4ZGg0dzhGdHBRQ0lIdmVTRE00clN2V3ZGZktROXRWTDRFZkpUdDc2cWliMFMyY2FBdDQwUHNGCi0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
    client-key-data: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0tCk1IY0NBUUVFSVBWS2JlQzJua2JaZ1UxZUNaS2NxUHpnSXd0MWxtOGcxZFNRaENoaHRURWVvQW9HQ0NxR1NNNDkKQXdFSG9VUURRZ0FFS2o3WWJpd0lseUxsZzFMWW5OVHZKK3JjZ08yMHlhdTdxV1FWWndzWm9naE5KUG82VmF5SAp0bHNGQ2lxUTBqMnpCTDM0RmZPWlpvMEpDd1c0SVZuOEN3PT0KLS0tLS1FTkQgRUMgUFJJVkFURSBLRVktLS0tLQo=
`)
)

func TestRunControllersTests(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "controllers suite")
}

var _ = ginkgo.Describe("Vcluster Controller test", func() {
	ginkgo.Context("Reconcile", func() {
		var (
			reconciler *controllers.VClusterReconciler
			ctx        context.Context
			scheme     *runtime.Scheme
			hemlClient *MockHelmClient
		)

		ginkgo.BeforeEach(func() {
			scheme = runtime.NewScheme()
			err := v1alpha1.AddToScheme(scheme)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = corev1.AddToScheme(scheme)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ctx = context.Background()
			hemlClient = &MockHelmClient{}
		})

		ginkgo.It("reconcile successfully", func() {
			virtualClusterVersion := "0.19"
			kubernetesVersion := "1.29.2"
			vCluster := &v1alpha1.VCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vcluster",
					Namespace: "default",
				},
				Spec: v1alpha1.VClusterSpec{
					VirtualClusterVersion: &virtualClusterVersion,
					KubernetesVersion:     &kubernetesVersion,
				},
			}
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: vCluster.Namespace,
					Name:      "vc-test-vcluster",
				},
				Data: map[string][]byte{
					"config": kubeconfigBytes,
				},
			}
			hemlClient.On("Upgrade").Return(nil)
			f := fakeclientset.NewSimpleClientset()

			_, err := f.CoreV1().ServiceAccounts("default").Create(context.Background(), &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			reconciler = &controllers.VClusterReconciler{
				Client:     fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(vCluster, secret).WithStatusSubresource(vCluster).Build(),
				HelmClient: hemlClient,
				Scheme:     scheme,
				ClientConfigGetter: &fakeConfigGetter{
					fake: f,
				},
				HTTPClientGetter: &fakeHTTPClientGetter{},
			}
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      vCluster.Name,
					Namespace: vCluster.Namespace,
				},
			}
			result, err := reconciler.Reconcile(ctx, req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result.RequeueAfter).Should(gomega.Equal(time.Minute))
		})
	})

})
