// Copyright 2026 [Copyright Holder]
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
// Author: [YOUR_NAME]

package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SNMPTrapCount tracks incoming Traps and Informs
	SNMPTrapCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "musubi",
			Subsystem: "snmp",
			Name:      "traps_total",
			Help:      "Total number of SNMP traps and informs received",
		},
		[]string{"target", "type"},
	)

	// SNMPRequestCount tracks GET, BULKGET, SET requests
	SNMPRequestCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "musubi",
			Subsystem: "snmp",
			Name:      "requests_total",
			Help:      "Total number of SNMP requests executed",
		},
		[]string{"target", "op"},
	)

	// SNMPNetworkBytes tracks network traffic bytes
	SNMPNetworkBytes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "musubi",
			Subsystem: "snmp",
			Name:      "traffic_bytes_total",
			Help:      "Total SNMP network traffic in bytes",
		},
		[]string{"target", "direction"},
	)

	// SNMPOperationDuration tracks SNMP round-trip latency
	SNMPOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "musubi",
			Subsystem: "snmp",
			Name:      "operation_duration_seconds",
			Help:      "SNMP request round-trip latency in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5},
		},
		[]string{"target", "op"},
	)

	// ScenarioJobCount tracks scenario executions
	ScenarioJobCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "musubi",
			Subsystem: "scenario",
			Name:      "jobs_total",
			Help:      "Total scenario execution jobs",
		},
		[]string{"scenario", "status"},
	)

	// StateTransitionsCount tracks state changes
	StateTransitionsCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "musubi",
			Subsystem: "state",
			Name:      "transitions_total",
			Help:      "Total state transitions recorded",
		},
		[]string{"target", "trigger"},
	)
)
