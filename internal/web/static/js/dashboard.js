// Copyright 2026 Musubi Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Author: sh0jitmy

(function() {
  const templates = {
    "snmp-get-descr": `name: snmp-get-sysdescr
target_locks: [spine1]
steps:
  - id: s1_get_descr
    target: spine1
    action: action.snmp_get
    params:
      oid: ".1.3.6.1.2.1.1.1.0"
teardown: []
`,
    "snmp-admin-toggle": `name: snmp-admin-toggle
target_locks: [spine1]
steps:
  - id: s1_set_down
    target: spine1
    action: action.snmp_set
    params:
      oid: ".1.3.6.1.2.1.2.2.1.7.1"
      type: int
      value: 2
  - id: s2_verify_down
    target: spine1
    action: action.snmp_get
    params:
      oid: ".1.3.6.1.2.1.2.2.1.7.1"
teardown:
  - id: td_restore_up
    target: spine1
    action: action.snmp_set
    params:
      oid: ".1.3.6.1.2.1.2.2.1.7.1"
      type: int
      value: 1
`,
    "snmp-bulk-walk": `name: snmp-bulk-interfaces
target_locks: [spine1]
steps:
  - id: s1_bulk_get
    target: spine1
    action: action.snmp_bulk_get
    params:
      oid: ".1.3.6.1.2.1.2.2.1"
      max_repetitions: 10
teardown: []
`
  };

  window.applyScenarioTemplate = function(templateKey) {
    const yamlInput = document.getElementById("scenario-dsl-input");
    const nameInput = document.getElementById("scenario-name-input");
    if (!yamlInput) return;

    if (templates[templateKey]) {
      yamlInput.value = templates[templateKey];
      if (nameInput) {
        nameInput.value = templateKey;
      }
    }
  };

  // Auto-refresh timestamp display
  function updateLiveClock() {
    const el = document.getElementById("live-clock");
    if (el) {
      const now = new Date();
      el.textContent = now.toLocaleTimeString();
    }
  }
  setInterval(updateLiveClock, 1000);
})();
