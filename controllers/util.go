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
	hadesv1alpha2 "github.com/kubeforge/hades/api/v1alpha2"
)

const (
	configLabelKey  = "hades.kubeforge.io/config"
	projectLabelKey = "hades.kubeforge.io/project"
)

// ensureLabels ensures that the keys and values given in ensureLabels are set in labels.
// Returns the (updated) labels map and a bool to indictae if the labels map was changed.
func ensureLabels(labels map[string]string, ensureLabels map[string]string) (map[string]string, bool) {
	if labels == nil {
		return ensureLabels, true
	}
	changed := false
	for key, value := range ensureLabels {
		if currentValue, ok := labels[key]; !ok || currentValue != value {
			labels[key] = value
			changed = true
		}
	}
	return labels, changed
}

// configSelectorLabels returns a set of labels that select the given config
func configSelectorLabels(config hadesv1alpha2.Config) map[string]string {
	return map[string]string{
		configLabelKey: config.Name,
	}
}

func projectSelectorLabels(project hadesv1alpha2.Project) map[string]string {
	return map[string]string{
		configLabelKey:  project.Spec.ConfigName,
		projectLabelKey: project.Name,
	}
}
