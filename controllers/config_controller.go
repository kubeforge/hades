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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hadesv1alpha2 "github.com/kubeforge/hades/api/v1alpha2"
)

const (
	projectOwnerKey = ".spec.configName"
	configLabelKey  = "hades.kubeforge.io/config"
)

// ConfigReconciler reconciles a Config object
type ConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=projects,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;watch;create;update;patch

// Reconcile reconciles the given request
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
				Labels: map[string]string{
					configLabelKey: config.Name,
				},
			},
			Rules: config.Spec.ClusterRules,
		}
		if err := ctrl.SetControllerReference(&config, &clusterRole, r.Scheme); err != nil {
			crLog.Error(err, "Unable to set clusterrole owner")
			return ctrl.Result{}, err
		}

		crLog.V(1).Info("Create clusterrole")
		if err := r.Create(ctx, &clusterRole); err != nil {
			crLog.Error(err, "Unable to create clusterrole")
			return ctrl.Result{}, err
		}
	} else {
		// Reconcile clusterrole
		clusterRoleLabels, updateNeeded := ensureLabel(clusterRole.GetLabels(), configLabelKey, config.Name)
		if !reflect.DeepEqual(config.Spec.ClusterRules, clusterRole.Rules) {
			updateNeeded = true
		}
		if updateNeeded {
			clusterRole.SetLabels(clusterRoleLabels)
			clusterRole.Rules = config.Spec.ClusterRules
			crLog.V(1).Info("Update clusterrole rules")
			if err := r.Update(ctx, &clusterRole); err != nil {
				crLog.Error(err, "Unable to update clusterrole rules")
				return ctrl.Result{}, err
			}
		}
	}

	// Reconcile projects
	var projectList hadesv1alpha2.ProjectList
	if err := r.List(ctx, &projectList, client.MatchingField(projectOwnerKey, req.Name)); err != nil {
		log.Error(err, "Unable to list projects")
		return ctrl.Result{}, err
	}
	projectRefs := make([]corev1.ObjectReference, 0, len(projectList.Items))
	for _, project := range projectList.Items {
		pLog := log.WithValues("project", project.Name)
		projectRef, err := ref.GetReference(r.Scheme, &project)
		if err != nil {
			pLog.Error(err, "Unable to make reference to project")
			continue
		}
		projectRefs = append(projectRefs, *projectRef)
	}
	if !reflect.DeepEqual(config.Status.Projects, projectRefs) {
		config.Status.Projects = projectRefs
		log.V(1).Info("Update config status")
		if err := r.Status().Update(ctx, &config); err != nil {
			log.Error(err, "Unable to update config status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Reconcile roles
	roleLabels := map[string]string{
		configLabelKey: config.Name,
	}
	var roleList rbacv1.RoleList
	if err := r.List(ctx, &roleList, client.MatchingLabels(roleLabels)); err != nil {
		log.Error(err, "Unable to list roles")
		return ctrl.Result{}, err
	}
	for _, role := range roleList.Items {
		if !reflect.DeepEqual(config.Spec.Rules, role.Rules) {
			rLog := log.WithValues("role", client.ObjectKey{Name: role.Name, Namespace: role.Namespace})
			role.Rules = config.Spec.Rules
			rLog.V(1).Info("Update role rules")
			if err := r.Update(ctx, &role); err != nil {
				rLog.Error(err, "Unable to update role")
				continue
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up this this reconciler with the given manager
func (r *ConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(&hadesv1alpha2.Project{}, projectOwnerKey, func(obj runtime.Object) []string {
		project := obj.(*hadesv1alpha2.Project)
		owner := metav1.GetControllerOf(project)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != hadesv1alpha2.GroupVersion.String() || owner.Kind != "Config" {
			return nil
		}
		return []string{owner.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&hadesv1alpha2.Config{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&hadesv1alpha2.Project{}).
		Complete(r)
}
