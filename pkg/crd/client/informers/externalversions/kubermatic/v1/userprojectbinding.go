// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	time "time"

	versioned "github.com/kubermatic/kubermatic/pkg/crd/client/clientset/versioned"
	internalinterfaces "github.com/kubermatic/kubermatic/pkg/crd/client/informers/externalversions/internalinterfaces"
	v1 "github.com/kubermatic/kubermatic/pkg/crd/client/listers/kubermatic/v1"
	kubermaticv1 "github.com/kubermatic/kubermatic/pkg/crd/kubermatic/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// UserProjectBindingInformer provides access to a shared informer and lister for
// UserProjectBindings.
type UserProjectBindingInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.UserProjectBindingLister
}

type userProjectBindingInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewUserProjectBindingInformer constructs a new informer for UserProjectBinding type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewUserProjectBindingInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredUserProjectBindingInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredUserProjectBindingInformer constructs a new informer for UserProjectBinding type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredUserProjectBindingInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KubermaticV1().UserProjectBindings().List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KubermaticV1().UserProjectBindings().Watch(options)
			},
		},
		&kubermaticv1.UserProjectBinding{},
		resyncPeriod,
		indexers,
	)
}

func (f *userProjectBindingInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredUserProjectBindingInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *userProjectBindingInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&kubermaticv1.UserProjectBinding{}, f.defaultInformer)
}

func (f *userProjectBindingInformer) Lister() v1.UserProjectBindingLister {
	return v1.NewUserProjectBindingLister(f.Informer().GetIndexer())
}
