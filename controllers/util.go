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

// ensureLabel ensures that a label with the given key and value exist in the labels map.
// Returns the (updated) labels map and a bool to indicate if the labels map was changed.
func ensureLabel(labels map[string]string, key, value string) (map[string]string, bool) {
	if labels == nil {
		return map[string]string{
			key: value,
		}, true
	}
	if currentValue, ok := labels[key]; ok && currentValue == value {
		return labels, false
	}
	labels[key] = value
	return labels, true
}

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
