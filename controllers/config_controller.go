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
)

// ConfigReconciler reconciles a Config object
type ConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=configs,verbs=get;watch;create;delete
// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=projects,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=list;update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;watch;create;update;patch;delete

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

	configLabels := configSelectorLabels(config)

	var clusterRole rbacv1.ClusterRole
	crLog := log.WithValues("clusterrole", config.Name)
	if err := r.Get(ctx, client.ObjectKey{Name: config.Name}, &clusterRole); err != nil {
		if !apierrors.IsNotFound(err) {
			crLog.Error(err, "Unable to fetch clusterrole")
			return ctrl.Result{}, err
		}

		clusterRole = rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:   config.Name,
				Labels: configLabels,
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
		clusterRoleLabels, updateNeeded := ensureLabels(clusterRole.GetLabels(), configLabels)
		if !reflect.DeepEqual(config.Spec.ClusterRules, clusterRole.Rules) {
			updateNeeded = true
		}
		if updateNeeded {
			clusterRole.SetLabels(clusterRoleLabels)
			clusterRole.Rules = config.Spec.ClusterRules
			crLog.V(1).Info("Update clusterrole")
			if err := r.Update(ctx, &clusterRole); err != nil {
				crLog.Error(err, "Unable to update clusterrole")
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
	projectSubjects := make([]rbacv1.Subject, len(projectList.Items)) // Subjects are needed for clusterrolebinding reconciliation
	for i, project := range projectList.Items {
		pLog := log.WithValues("project", project.Name)
		projectSubjects[i] = rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      project.Name,
			Namespace: project.Name,
		}
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
	var roleList rbacv1.RoleList
	if err := r.List(ctx, &roleList, client.MatchingLabels(configLabels)); err != nil {
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

	// Reconcile clusterrolebindings
	roleRef := rbacv1.RoleRef{
		APIGroup: clusterRole.GroupVersionKind().Group,
		Kind:     "ClusterRole",
		Name:     clusterRole.Name,
	}

	var clusterRoleBinding rbacv1.ClusterRoleBinding
	crbLog := log.WithValues("clusterrolebinding", config.Name)
	if err := r.Get(ctx, client.ObjectKey{Name: config.Name}, &clusterRoleBinding); err != nil {
		if !apierrors.IsNotFound(err) {
			crbLog.Error(err, "Unable to fetch clusterrolebinding")
			return ctrl.Result{}, err
		}

		clusterRoleBinding = rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:   config.Name,
				Labels: configLabels,
			},
			RoleRef:  roleRef,
			Subjects: projectSubjects,
		}
		if err := ctrl.SetControllerReference(&config, &clusterRoleBinding, r.Scheme); err != nil {
			crbLog.Error(err, "Unable to set clusterrolebinding owner")
			return ctrl.Result{}, err
		}

		crbLog.V(1).Info("Create clusterrolebinding")
		if err := r.Create(ctx, &clusterRoleBinding); err != nil {
			crbLog.Error(err, "Unable to create clusterrolebinding")
			return ctrl.Result{}, err
		}
	} else {
		// Reconcile clusterrolebinding
		clusterRoleBindingLabels := clusterRoleBinding.GetLabels()
		clusterRoleBindingLabels, updateNeeded := ensureLabels(clusterRoleBindingLabels, configLabels)
		if !reflect.DeepEqual(roleRef, clusterRoleBinding.RoleRef) {
			updateNeeded = true
		}
		if !reflect.DeepEqual(projectSubjects, clusterRoleBinding.Subjects) {
			updateNeeded = true
		}
		if updateNeeded {
			clusterRoleBinding.SetLabels(clusterRoleBindingLabels)
			clusterRoleBinding.RoleRef = roleRef
			clusterRoleBinding.Subjects = projectSubjects
			crbLog.V(1).Info("Update clusterrolebinding")
			if err := r.Update(ctx, &clusterRoleBinding); err != nil {
				crbLog.Error(err, "Unable to update clusterrolebinding")
				return ctrl.Result{}, err
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
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&hadesv1alpha2.Project{}).
		Complete(r)
}
