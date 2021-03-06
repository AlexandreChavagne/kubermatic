/*
Copyright 2020 The Kubermatic Kubernetes Platform contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nodeportproxy

import (
	"context"
	"fmt"

	kubermaticv1 "github.com/kubermatic/kubermatic/pkg/crd/kubermatic/v1"
	"github.com/kubermatic/kubermatic/pkg/resources"
	"github.com/kubermatic/kubermatic/pkg/resources/reconciling"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	name               = "nodeport-proxy"
	imageName          = "kubermatic/nodeport-proxy"
	envoyAppLabelValue = name + "-envoy"

	// NodePortPRoxyExposeNamespacedAnnotationKey is the annotation key used to indicate that
	// a service should be exposed by the namespaced NodeportProxy instance.
	// We use it when clusters get exposed via a LoadBalancer, to allow re-using that LoadBalancer
	// for both the kube-apiserver and the openVPN server
	NodePortProxyExposeNamespacedAnnotationKey = "nodeport-proxy.k8s.io/expose-namespaced"
)

var (
	defaultResourceRequirements = map[string]*corev1.ResourceRequirements{
		"envoy-manager": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("48Mi"),
			},
		},
		"envoy": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		"lb-updater": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		},
	}
)

func EnsureResources(ctx context.Context, client ctrlruntimeclient.Client, data nodePortProxyData) error {
	image := data.ImageRegistry("quay.io") + "/" + imageName + ":" + resources.KUBERMATICCOMMIT
	namespace := data.Cluster().Status.NamespaceName
	if namespace == "" {
		return fmt.Errorf(".Status.NamespaceName is empty for cluster %q", data.Cluster().Name)
	}

	err := reconciling.ReconcileServiceAccounts(
		ctx, []reconciling.NamedServiceAccountCreatorGetter{serviceAccount()}, namespace, client)
	if err != nil {
		return fmt.Errorf("failed to ensure ServiceAccount: %v", err)
	}

	err = reconciling.ReconcileRoles(
		ctx, []reconciling.NamedRoleCreatorGetter{role()}, namespace, client)
	if err != nil {
		return fmt.Errorf("failed to ensure Role: %v", err)
	}

	err = reconciling.ReconcileRoleBindings(
		ctx, []reconciling.NamedRoleBindingCreatorGetter{roleBinding(namespace)}, namespace, client)
	if err != nil {
		return fmt.Errorf("failed to ensure RoleBinding: %v", err)
	}

	deployments := []reconciling.NamedDeploymentCreatorGetter{deploymentEnvoy(image, data),
		deploymentLBUpdater(image)}
	err = reconciling.ReconcileDeployments(ctx, deployments, namespace, client)
	if err != nil {
		return fmt.Errorf("failed to reconcile Deployments: %v", err)
	}

	err = reconciling.ReconcilePodDisruptionBudgets(
		ctx, []reconciling.NamedPodDisruptionBudgetCreatorGetter{podDisruptionBudget()}, namespace, client)
	if err != nil {
		return fmt.Errorf("failed to reconcile PodDisruptionBudget: %v", err)
	}
	return nil
}

type nodePortProxyData interface {
	ImageRegistry(string) string
	Cluster() *kubermaticv1.Cluster
}

func serviceAccount() reconciling.NamedServiceAccountCreatorGetter {
	return func() (string, reconciling.ServiceAccountCreator) {
		return name, func(sa *corev1.ServiceAccount) (*corev1.ServiceAccount, error) {
			return sa, nil
		}
	}
}

func role() reconciling.NamedRoleCreatorGetter {
	return func() (string, reconciling.RoleCreator) {
		return name, func(r *rbacv1.Role) (*rbacv1.Role, error) {
			r.Rules = []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"services", "pods"},
					Verbs:     []string{"list", "get", "watch"},
				},
				{
					APIGroups:     []string{""},
					Resources:     []string{"services"},
					ResourceNames: []string{resources.FrontLoadBalancerServiceName},
					Verbs:         []string{"update"},
				},
			}
			return r, nil
		}
	}
}

func roleBinding(ns string) reconciling.NamedRoleBindingCreatorGetter {
	return func() (string, reconciling.RoleBindingCreator) {
		return name, func(r *rbacv1.RoleBinding) (*rbacv1.RoleBinding, error) {
			r.Subjects = []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      name,
					Namespace: ns,
				},
			}
			r.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     name,
			}
			return r, nil
		}
	}
}

func deploymentEnvoy(image string, data nodePortProxyData) reconciling.NamedDeploymentCreatorGetter {
	volumeMountNameEnvoyConfig := "envoy-config"
	name := envoyAppLabelValue
	return func() (string, reconciling.DeploymentCreator) {
		return name, func(d *appsv1.Deployment) (*appsv1.Deployment, error) {
			d.Labels = resources.BaseAppLabels(name, nil)
			d.Spec.Replicas = resources.Int32(2)
			d.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: resources.BaseAppLabels(name, nil)}
			d.Spec.Template.Labels = resources.BaseAppLabels(name, nil)
			d.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
				{Name: resources.ImagePullSecretName},
			}

			d.Spec.Template.Spec.InitContainers = []corev1.Container{
				{
					Name:  "copy-envoy-config",
					Image: image,
					Command: []string{
						"/bin/cp",
						"/envoy.yaml",
						"/etc/envoy/envoy.yaml",
					},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      volumeMountNameEnvoyConfig,
						MountPath: "/etc/envoy",
					}},
				},
			}

			d.Spec.Template.Spec.Containers = []corev1.Container{{
				Name:  "envoy-manager",
				Image: image,
				Command: []string{"/envoy-manager",
					"-listen-address=:8001",
					"-envoy-node-name=kube",
					"-envoy-admin-port=9001",
					"-envoy-stats-port=8002",
					"-expose-annotation-key=" + NodePortProxyExposeNamespacedAnnotationKey,
					"-namespace=$(MY_NAMESPACE)"},
				Env: []corev1.EnvVar{{
					Name: "MY_NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					}},
				}},
			}, {
				Name:  "envoy",
				Image: data.ImageRegistry("docker.io") + "/envoyproxy/envoy-alpine:v1.13.0",
				Command: []string{
					"/usr/local/bin/envoy",
					"-c",
					"/etc/envoy/envoy.yaml",
					"--service-cluster",
					"cluster0",
					"--service-node",
					"kube",
				},
				Lifecycle: &corev1.Lifecycle{
					PreStop: &corev1.Handler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"wget",
								"-q0",
								"http://127.0.0.1:9001/healthcheck/fail",
							},
						},
					},
				},
				ReadinessProbe: &corev1.Probe{
					FailureThreshold: 3,
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "healthz",
							Port:   intstr.FromInt(8002),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					PeriodSeconds:    3,
					SuccessThreshold: 1,
					TimeoutSeconds:   1,
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      volumeMountNameEnvoyConfig,
					MountPath: "/etc/envoy",
				}},
			}}
			err := resources.SetResourceRequirements(d.Spec.Template.Spec.Containers, defaultResourceRequirements, nil, d.Annotations)
			if err != nil {
				return nil, fmt.Errorf("failed to set resource requirements: %v", err)
			}
			d.Spec.Template.Spec.Volumes = []corev1.Volume{{
				Name: volumeMountNameEnvoyConfig,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			}}
			d.Spec.Template.Spec.ServiceAccountName = "nodeport-proxy"
			return d, nil
		}
	}
}

func deploymentLBUpdater(image string) reconciling.NamedDeploymentCreatorGetter {
	name := name + "-lb-updater"
	return func() (string, reconciling.DeploymentCreator) {
		return name, func(d *appsv1.Deployment) (*appsv1.Deployment, error) {
			d.Labels = resources.BaseAppLabels(name, nil)
			d.Spec.Replicas = resources.Int32(1)
			d.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: resources.BaseAppLabels(name, nil)}
			d.Spec.Template.Labels = resources.BaseAppLabels(name, nil)
			d.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
				{Name: resources.ImagePullSecretName},
			}

			d.Spec.Template.Spec.Containers = []corev1.Container{{
				Name: "lb-updater",
				Command: []string{
					"/lb-updater",
					"-lb-namespace=$(MY_NAMESPACE)",
					"-lb-name=" + resources.FrontLoadBalancerServiceName,
					"-expose-annotation-key=" + NodePortProxyExposeNamespacedAnnotationKey,
					"-namespaced=true",
				},
				Image: image,
				Env: []corev1.EnvVar{{
					Name: "MY_NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					}},
				}},
			}}
			err := resources.SetResourceRequirements(d.Spec.Template.Spec.Containers, defaultResourceRequirements, nil, d.Annotations)
			if err != nil {
				return nil, fmt.Errorf("failed to set resource requirements: %v", err)
			}
			d.Spec.Template.Spec.ServiceAccountName = "nodeport-proxy"

			return d, nil
		}
	}
}

func podDisruptionBudget() reconciling.NamedPodDisruptionBudgetCreatorGetter {
	maxUnavailable := intstr.FromInt(1)
	return func() (string, reconciling.PodDisruptionBudgetCreator) {
		return name + "-envoy", func(pdb *policyv1beta1.PodDisruptionBudget) (*policyv1beta1.PodDisruptionBudget, error) {
			pdb.Spec.MaxUnavailable = &maxUnavailable
			pdb.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: resources.BaseAppLabels(envoyAppLabelValue, nil),
			}
			return pdb, nil
		}
	}
}

// FrontLoadBalancerServiceCreator returns the creator for the LoadBalancer that fronts apiserver
// and openVPN when using exposeStrategy=LoadBalancer
func FrontLoadBalancerServiceCreator() reconciling.NamedServiceCreatorGetter {
	return func() (string, reconciling.ServiceCreator) {
		return resources.FrontLoadBalancerServiceName, func(s *corev1.Service) (*corev1.Service, error) {
			// We don't actually manage this service, that is done by the nodeport proxy, we just
			// must make sure that it exists
			s.Spec.Type = corev1.ServiceTypeLoadBalancer
			// Services need at least one port to be valid, so create it initially
			if len(s.Spec.Ports) == 0 {
				s.Spec.Ports = []corev1.ServicePort{
					{
						Name:       "secure",
						Port:       443,
						Protocol:   corev1.ProtocolTCP,
						TargetPort: intstr.FromInt(443),
					},
				}
			}

			s.Spec.Selector = resources.BaseAppLabels(envoyAppLabelValue, nil)
			return s, nil
		}
	}
}
