/*

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

package controllers

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hadesv1alpha2 "github.com/kubeforge/hades/api/v1alpha2"
)

// ConfigReconciler reconciles a Config object
type ConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;watch;create;update;patch

func (r *ConfigReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("config", req.Name)

	var config hadesv1alpha2.Config
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Unable to fetch config")
			return ctrl.Result{}, err
		}
		log.V(1).Info("Unable to find config")
		return ctrl.Result{}, nil
	}

	var clusterRole rbacv1.ClusterRole
	crLog := log.WithValues("clusterrole", config.Name)
	if err := r.Get(ctx, client.ObjectKey{Name: config.Name}, &clusterRole); err != nil {
		if !apierrors.IsNotFound(err) {
			crLog.Error(err, "Unable to fetch clusterrole")
			return ctrl.Result{}, err
		}

		clusterRole = rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: config.Name,
			},
			Rules: config.Spec.ClusterRules,
		}
		if err := ctrl.SetControllerReference(&config, &clusterRole, r.Scheme); err != nil {
			crLog.Error(err, "Unable to set clusterrole controller reference")
			return ctrl.Result{}, err
		}

		crLog.V(1).Info("Create clusterrole")
		if err := r.Create(ctx, &clusterRole); err != nil {
			crLog.Error(err, "Unable to create clusterrole")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile rules
	if !reflect.DeepEqual(config.Spec.ClusterRules, clusterRole.Rules) {
		clusterRole.Rules = config.Spec.ClusterRules
		log.V(1).Info("Update clusterrole rules")
		if err := r.Update(ctx, &clusterRole); err != nil {
			log.Error(err, "Unable to update clusterrole")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hadesv1alpha2.Config{}).
		Owns(&rbacv1.ClusterRole{}).
		Complete(r)
}
