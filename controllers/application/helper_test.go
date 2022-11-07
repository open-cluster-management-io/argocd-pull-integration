/*
Copyright 2022.

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

package application

import (
	"reflect"
	"testing"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_containsValidPullLabel(t *testing.T) {
	type args struct {
		application argov1alpha1.Application
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "valid pull label",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{LabelKeyPull: "true"},
					},
				},
			},
			want: true,
		},
		{
			name: "valid pull label case",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{LabelKeyPull: "True"},
					},
				},
			},
			want: true,
		},
		{
			name: "invalid pull label",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{LabelKeyPull + "a": "true"},
					},
				},
			},
			want: false,
		},
		{
			name: "empty value",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{LabelKeyPull: ""},
					},
				},
			},
			want: false,
		},
		{
			name: "false value",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{LabelKeyPull: "false"},
					},
				},
			},
			want: false,
		},
		{
			name: "no pull label",
			args: args{
				argov1alpha1.Application{},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsValidPullLabel(tt.args.application); got != tt.want {
				t.Errorf("containsValidPullLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_containsValidPullAnnotation(t *testing.T) {
	type args struct {
		application argov1alpha1.Application
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "valid pull annotation",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{AnnotationKeyOCMManagedCluster: "cluster1"},
					},
				},
			},
			want: true,
		},
		{
			name: "invalid pull annotation",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{AnnotationKeyOCMManagedCluster + "a": "cluster1"},
					},
				},
			},
			want: false,
		},
		{
			name: "empty value",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{AnnotationKeyOCMManagedCluster: ""},
					},
				},
			},
			want: false,
		},
		{
			name: "no pull annotation",
			args: args{
				argov1alpha1.Application{},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsValidPullAnnotation(tt.args.application); got != tt.want {
				t.Errorf("containsValidPullAnnotation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generateAppNamespace(t *testing.T) {
	type args struct {
		application argov1alpha1.Application
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "annotation only",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{AnnotationKeyOCMManagedClusterAppNamespace: "gitops"},
					},
				},
			},
			want: "gitops",
		},
		{
			name: "annotation and namespace",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{AnnotationKeyOCMManagedClusterAppNamespace: "gitops"},
						Namespace:   "argocd",
					},
				},
			},
			want: "gitops",
		},
		{
			name: "namespace only",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "gitops",
					},
				},
			},
			want: "gitops",
		},
		{
			name: "annotation and namespace not found",
			args: args{
				argov1alpha1.Application{},
			},
			want: "argocd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateAppNamespace(tt.args.application); got != tt.want {
				t.Errorf("generateAppNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generateManifestWorkName(t *testing.T) {
	type args struct {
		application argov1alpha1.Application
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "generate name",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Name: "app1",
						UID:  "abcdefghijk",
					},
				},
			},
			want: "app1-abcde",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateManifestWorkName(tt.args.application); got != tt.want {
				t.Errorf("generateManifestWorkName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_prepareApplicationForWorkPayload(t *testing.T) {
	type args struct {
		application argov1alpha1.Application
	}
	tests := []struct {
		name string
		args args
		want argov1alpha1.Application
	}{
		{
			name: "modified app",
			args: args{
				argov1alpha1.Application{
					ObjectMeta: v1.ObjectMeta{
						Name:       "app1",
						Finalizers: []string{"app1-final"},
					},
				},
			},
			want: argov1alpha1.Application{
				ObjectMeta: v1.ObjectMeta{
					Name:       "app1",
					Namespace:  "argocd",
					Finalizers: []string{"app1-final"},
				},
				Spec: argov1alpha1.ApplicationSpec{
					Destination: argov1alpha1.ApplicationDestination{
						Name:   "",
						Server: "https://kubernetes.default.svc",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prepareApplicationForWorkPayload(tt.args.application)
			if !reflect.DeepEqual(got.Name, tt.want.Name) {
				t.Errorf("prepareApplicationForWorkPayload() = %v, want %v", got.Name, tt.want.Name)
			}
			if !reflect.DeepEqual(got.Finalizers, tt.want.Finalizers) {
				t.Errorf("prepareApplicationForWorkPayload() = %v, want %v", got.Finalizers, tt.want.Finalizers)
			}
			if !reflect.DeepEqual(got.Namespace, tt.want.Namespace) {
				t.Errorf("prepareApplicationForWorkPayload() = %v, want %v", got.Namespace, tt.want.Namespace)
			}
			if !reflect.DeepEqual(got.Spec.Destination, tt.want.Spec.Destination) {
				t.Errorf("prepareApplicationForWorkPayload() = %v, want %v", got.Spec.Destination, tt.want.Spec.Destination)
			}
		})
	}
}

func Test_generateManifestWork(t *testing.T) {
	app := argov1alpha1.Application{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app1",
			Namespace: "argocd",
			OwnerReferences: []v1.OwnerReference{{
				APIVersion: "argoproj.io/v1alpha1",
				Kind:       "ApplicationSet",
				Name:       "appset1",
			}},
			Finalizers: []string{argov1alpha1.ResourcesFinalizerName},
		},
	}

	type args struct {
		name        string
		namespace   string
		application argov1alpha1.Application
	}
	type results struct {
		workLabel map[string]string
		workAnno  map[string]string
	}
	tests := []struct {
		name string
		args args
		want results
	}{
		{
			name: "sunny",
			args: args{
				name:        "app1-abcde",
				namespace:   "cluster1",
				application: app,
			},
			want: results{
				workLabel: map[string]string{LabelKeyAppSet: "true"},
				workAnno:  map[string]string{AnnotationKeyAppSet: "argocd/appset1"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateManifestWork(tt.args.name, tt.args.namespace, tt.args.application)
			if !reflect.DeepEqual(got.Annotations, tt.want.workAnno) {
				t.Errorf("generateManifestWork() = %v, want %v", got.Annotations, tt.want.workAnno)
			}
			if !reflect.DeepEqual(got.Labels, tt.want.workLabel) {
				t.Errorf("generateManifestWork() = %v, want %v", got.Labels, tt.want.workLabel)
			}
		})
	}
}
