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
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hadesv1alpha2 "github.com/kubeforge/hades/api/v1alpha2"
)

const (
	projectLabelKey = "hades.kubeforge.io/project"
)

// ProjectReconciler reconciles a Project object
type ProjectReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=projects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hades.kubeforge.io,resources=configs,verbs=get
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;watch;create;update;patch

// Reconcile reconciles the given request
func (r *ProjectReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("project", req.Name)

	var project hadesv1alpha2.Project
	if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Unable to fetch project")
			return ctrl.Result{}, err
		}
		log.V(1).Info("Unable to find project")
		return ctrl.Result{}, nil
	}

	var config hadesv1alpha2.Config
	if err := r.Get(ctx, client.ObjectKey{Name: project.Spec.ConfigName}, &config); err != nil {
		cLog := log.WithValues("config", project.Spec.ConfigName)
		if !apierrors.IsNotFound(err) {
			cLog.Error(err, "Unable to fetch config")
			return ctrl.Result{}, err
		}
		cLog.Info("Unable to find config")
		return ctrl.Result{}, r.failWithConfigError(ctx, nil, &project, "ConfigNotFound", "No config found with the given name")
	}

	// Reconcile owner reference to config
	if !metav1.IsControlledBy(&project, &config) {
		owner := metav1.GetControllerOf(&project)
		if owner != nil {
			// Project references wrong owner
			owners := project.GetOwnerReferences()
			oi := -1
			for i, o := range owners {
				if o.APIVersion == owner.APIVersion && o.Kind == owner.Kind && o.Name == owner.Name {
					oi = i
					break
				}
			}
			if oi == -1 {
				err := fmt.Errorf("invalid owner reference")
				log.Error(err, "Unable to fix wrong owner")
				return ctrl.Result{}, r.failWithConfigError(ctx, err, &project, "InvalidProjectOwner", "Project is configured with invalid owner")
			}
			// Remove owner
			owners[len(owners)-1], owners[oi] = owners[oi], owners[len(owners)-1]
			project.SetOwnerReferences(owners[:len(owners)-1])
		}
		if err := ctrl.SetControllerReference(&config, &project, r.Scheme); err != nil {
			log.Error(err, "Unable to set project owner")
			return ctrl.Result{}, r.failWithConfigError(ctx, err, &project, "InvalidProjectOwner", "Unable to set project owner")
		}

		log.V(1).Info("Update project owners")
		if err := r.Update(ctx, &project); err != nil {
			log.Error(err, "Unable to update project owners")
			return ctrl.Result{}, r.failWithConfigError(ctx, err, &project, "InvalidProjectOwner", "Unable to update project owners")
		}
		return ctrl.Result{}, nil
	}
	setProjectCondition(&project, hadesv1alpha2.ProjectConditionConfigured, corev1.ConditionTrue, "ConfigFound", "Config controls this project")
	log.V(1).Info("Update project status")
	if err := r.Status().Update(ctx, &project); err != nil {
		log.Error(err, "Unable to update project status")
		return ctrl.Result{}, err
	}

	var namespace corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: project.Name}, &namespace); err != nil {
		nLog := log.WithValues("namespace", project.Name)
		if !apierrors.IsNotFound(err) {
			nLog.Error(err, "Unable to fetch namespace")
			return ctrl.Result{}, err
		}

		namespace = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: project.Name,
			},
		}
		if err := ctrl.SetControllerReference(&project, &namespace, r.Scheme); err != nil {
			nLog.Error(err, "Unable to set namespace owner")
			return ctrl.Result{}, err
		}

		nLog.V(1).Info("Create namespace")
		if err := r.Create(ctx, &namespace); err != nil {
			nLog.Error(err, "Unable to create namespace")
			return ctrl.Result{}, err
		}
	}

	var role rbacv1.Role
	rKey := client.ObjectKey{Name: project.Name, Namespace: namespace.Name}
	rLog := log.WithValues("role", rKey)
	rLabels := map[string]string{
		projectLabelKey: project.Name,
		configLabelKey:  config.Name,
	}
	if err := r.Get(ctx, rKey, &role); err != nil {
		if !apierrors.IsNotFound(err) {
			rLog.Error(err, "Unable to fetch role")
			return ctrl.Result{}, err
		}

		role = rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      project.Name,
				Namespace: namespace.Name,
				Labels:    rLabels,
			},
			Rules: config.Spec.Rules,
		}
		if err := ctrl.SetControllerReference(&project, &role, r.Scheme); err != nil {
			rLog.Error(err, "Unable to set role owner")
			return ctrl.Result{}, err
		}

		rLog.V(1).Info("Create role")
		if err := r.Create(ctx, &role); err != nil {
			rLog.Error(err, "Unable to create role")
			return ctrl.Result{}, err
		}
	} else {
		// Reconcile role
		roleLabels := role.GetLabels()
		roleLabels, updateNeeded := ensureLabels(roleLabels, rLabels)
		if !reflect.DeepEqual(config.Spec.Rules, role.Rules) {
			updateNeeded = true
		}
		if updateNeeded {
			role.SetLabels(roleLabels)
			role.Rules = config.Spec.Rules
			rLog.V(1).Info("Update role rules")
			if err := r.Update(ctx, &role); err != nil {
				rLog.Error(err, "Unable to update role")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up this this reconciler with the given manager
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hadesv1alpha2.Project{}).
		Owns(&corev1.Namespace{}).
		Owns(&rbacv1.Role{}).
		Complete(r)
}

func getProjectCondition(project *hadesv1alpha2.Project, cType hadesv1alpha2.ProjectConditionType) *hadesv1alpha2.ProjectCondition {
	for _, c := range project.Status.Conditions {
		if c.Type == cType {
			return &c
		}
	}
	return nil
}

func setProjectCondition(project *hadesv1alpha2.Project, cType hadesv1alpha2.ProjectConditionType, status corev1.ConditionStatus, reason, message string) {
	now := metav1.Time{Time: time.Now()}
	for i, c := range project.Status.Conditions {
		if c.Type == cType {
			if c.Status != status {
				c.Status = status
				c.LastTransitionTime = now
			}
			c.LastProbeTime = now
			c.Reason = reason
			c.Message = message

			project.Status.Conditions[i] = c
			return
		}
	}

	project.Status.Conditions = append(project.Status.Conditions, hadesv1alpha2.ProjectCondition{
		Type:               cType,
		Status:             status,
		LastProbeTime:      now,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})
}

// failWithConfigError updates the project status with a "Configured" condition set to false.
// returns the 'fail' argument or any errors that might pop up during the status update.
func (r *ProjectReconciler) failWithConfigError(ctx context.Context, fail error, project *hadesv1alpha2.Project, reason, message string) error {
	log := r.Log.WithValues("project", project.Name)
	setProjectCondition(project, hadesv1alpha2.ProjectConditionConfigured, corev1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, project); err != nil {
		log.Error(err, "Unable to update project status")
		return err
	}
	return fail
}
