package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/loft-sh/vcluster/cmd/vclusterctl/cmd"
	"github.com/loft-sh/vcluster/cmd/vclusterctl/flags"
	"github.com/loft-sh/vcluster/cmd/vclusterctl/log"
	logutil "github.com/loft-sh/vcluster/pkg/util/log"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRunE2ETests(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "e2e suite")
}

var _ = ginkgo.Describe("e2e test", func() {
	ginkgo.Context("E2E", func() {

		var (
			vclusterConfig  *rest.Config
			vclusterClient  *kubernetes.Clientset
			vKubeconfigFile *os.File
			ctx             context.Context
		)

		ginkgo.BeforeEach(func() {
			ctx = context.TODO()
			ctrl.SetLogger(logutil.NewLog(0))
			l := log.GetInstance()
			scheme := runtime.NewScheme()

			// run port forwarder and retrieve kubeconfig for the vcluster
			var err error
			vKubeconfigFile, err = os.CreateTemp(os.TempDir(), "vcluster_e2e_kubeconfig_")

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			connectCmd := cmd.ConnectCmd{
				Log: l,
				GlobalFlags: &flags.GlobalFlags{
					Namespace: "vcluster",
					Debug:     true,
				},
				KubeConfig: vKubeconfigFile.Name(),
				LocalPort:  14550, // choosing a port that usually should be unused
			}
			err = connectCmd.Connect(ctx, "vcluster", nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = wait.PollUntilContextTimeout(ctx, time.Second, time.Minute*5, false, func(ctx context.Context) (bool, error) {
				output, err := os.ReadFile(vKubeconfigFile.Name())
				if err != nil {
					return false, nil
				}

				// try to parse config from file with retry because the file content might not be written
				vclusterConfig, err = clientcmd.RESTConfigFromKubeConfig(output)
				if err != nil {
					return false, err
				}
				vclusterConfig.Timeout = time.Minute

				// create kubernetes client using the config retry in case port forwarding is not ready yet
				vclusterClient, err = kubernetes.NewForConfig(vclusterConfig)
				if err != nil {
					return false, err
				}

				_, err = client.New(vclusterConfig, client.Options{Scheme: scheme})
				if err != nil {
					return false, err
				}

				// try to use the client with retry in case port forwarding is not ready yet
				_, err = vclusterClient.CoreV1().ServiceAccounts("default").Get(ctx, "default", metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return true, nil
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("Deploys Workload to VirtualCluster successfully", func() {
			replicas := int32(2)
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-deployment",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "example",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "example",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
								},
							},
						},
					},
				},
			}

			_, err := vclusterClient.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("Scale Deployment successfully", func() {
			deployment, err := vclusterClient.AppsV1().Deployments("default").Get(ctx, "example-deployment", metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			replicas := int32(5)
			deployment.Spec.Replicas = &replicas
			_, err = vclusterClient.AppsV1().Deployments("default").Update(ctx, deployment, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("Delete VirtualCluster successfully", func() {
			_, err := vclusterClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "vcluster",
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Delete the VirtualCluster
			err = vclusterClient.CoreV1().Namespaces().Delete(ctx, "vcluster", metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			defer os.Remove(vKubeconfigFile.Name())
		})
	})

})
