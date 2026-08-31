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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli/v2"
)

var defaultEndpoint = "http://localhost:8080"

func main() {
	app := &cli.App{
		Name:    "musubi-cli",
		Usage:   "Official CLI for Musubi Network Orchestration Engine",
		Version: "1.0.0",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "endpoint",
				Aliases: []string{"e"},
				Value:   defaultEndpoint,
				Usage:   "Musubi server base URL",
				EnvVars: []string{"MUSUBI_ENDPOINT"},
			},
			&cli.StringFlag{
				Name:    "token",
				Aliases: []string{"t"},
				Usage:   "Bearer JWT token",
				EnvVars: []string{"MUSUBI_TOKEN"},
			},
		},
		Commands: []*cli.Command{
			targetCommand(),
			scenarioCommand(),
			jobCommand(),
			stateCommand(),
			auditCommand(),
			systemCommand(),
			maintenanceCommand(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func targetCommand() *cli.Command {
	return &cli.Command{
		Name:  "target",
		Usage: "Manage network target devices and inventory",
		Subcommands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List all network targets",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/targets")
				},
			},
			{
				Name:  "drain",
				Usage: "Gracefully drain a target",
				Action: func(c *cli.Context) error {
					if c.NArg() == 0 {
						return fmt.Errorf("target name required")
					}
					return httpPost(c, fmt.Sprintf("/v1/targets/%s/drain", c.Args().First()), nil)
				},
			},
			{
				Name:  "ping",
				Usage: "Ping target SNMP probe",
				Action: func(c *cli.Context) error {
					if c.NArg() == 0 {
						return fmt.Errorf("target name required")
					}
					return httpPost(c, fmt.Sprintf("/v1/targets/%s/ping", c.Args().First()), nil)
				},
			},
			{
				Name:  "delete",
				Usage: "Delete a target",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "force", Usage: "Force soft delete"},
					&cli.BoolFlag{Name: "force-abort", Usage: "Force abort active jobs"},
					&cli.BoolFlag{Name: "cleanup-scenarios", Usage: "Cascade cleanup scenarios"},
				},
				Action: func(c *cli.Context) error {
					if c.NArg() == 0 {
						return fmt.Errorf("target name required")
					}
					path := fmt.Sprintf("/v1/targets/%s?force=%t&force_abort=%t&cleanup_scenarios=%t",
						c.Args().First(), c.Bool("force"), c.Bool("force-abort"), c.Bool("cleanup-scenarios"))
					return httpDelete(c, path)
				},
			},
		},
	}
}

func scenarioCommand() *cli.Command {
	return &cli.Command{
		Name:  "scenario",
		Usage: "Manage test scenarios and YAML DSLs",
		Subcommands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List registered scenarios",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/scenarios")
				},
			},
			{
				Name:  "orphans",
				Usage: "Detect orphan scenarios referencing missing targets",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/scenarios/orphans")
				},
			},
			{
				Name:  "cleanup",
				Usage: "Batch cleanup orphan scenarios",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "dry-run", Usage: "Preview cleanup without deleting"},
					&cli.BoolFlag{Name: "archive", Value: true, Usage: "Export backup archive before delete"},
				},
				Action: func(c *cli.Context) error {
					payload := map[string]any{
						"dry_run": c.Bool("dry-run"),
						"archive": c.Bool("archive"),
					}
					return httpPost(c, "/v1/scenarios/cleanups", payload)
				},
			},
			{
				Name:  "run",
				Usage: "Execute scenario",
				Action: func(c *cli.Context) error {
					if c.NArg() == 0 {
						return fmt.Errorf("scenario name required")
					}
					return httpPost(c, fmt.Sprintf("/v1/scenarios/%s/runs", c.Args().First()), map[string]any{})
				},
			},
		},
	}
}

func jobCommand() *cli.Command {
	return &cli.Command{
		Name:  "job",
		Usage: "Monitor and control scenario jobs",
		Subcommands: []*cli.Command{
			{
				Name:  "status",
				Usage: "Get job execution status",
				Action: func(c *cli.Context) error {
					if c.NArg() == 0 {
						return fmt.Errorf("job ID required")
					}
					return httpGet(c, fmt.Sprintf("/v1/jobs/%s", c.Args().First()))
				},
			},
			{
				Name:  "cancel",
				Usage: "Cancel / Abort a running job",
				Action: func(c *cli.Context) error {
					if c.NArg() == 0 {
						return fmt.Errorf("job ID required")
					}
					return httpPost(c, fmt.Sprintf("/v1/jobs/%s/cancels", c.Args().First()), nil)
				},
			},
		},
	}
}

func stateCommand() *cli.Command {
	return &cli.Command{
		Name:  "state",
		Usage: "Inspect SNMP state cache and state transitions",
		Subcommands: []*cli.Command{
			{
				Name:  "raw",
				Usage: "Query observed raw SNMP state",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/states/raw")
				},
			},
			{
				Name:  "derived",
				Usage: "Query derived state context",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/states/derived")
				},
			},
			{
				Name:  "export-transitions",
				Usage: "Export state change diff timeline",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/states/transitions/exports")
				},
			},
		},
	}
}

func auditCommand() *cli.Command {
	return &cli.Command{
		Name:  "audit",
		Usage: "Manage audit logs and export evidence",
		Subcommands: []*cli.Command{
			{
				Name:  "logs",
				Usage: "List operation audit logs",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/audit/logs")
				},
			},
			{
				Name:  "export",
				Usage: "Export tamper-evident audit evidence package (ZIP)",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/audit/exports")
				},
			},
		},
	}
}

func systemCommand() *cli.Command {
	return &cli.Command{
		Name:  "system",
		Usage: "System backup, restore, and diagnostics",
		Subcommands: []*cli.Command{
			{
				Name:  "health",
				Usage: "Deep health check",
				Action: func(c *cli.Context) error {
					return httpGet(c, "/v1/system/healths")
				},
			},
			{
				Name:  "backup",
				Usage: "Create full system backup",
				Action: func(c *cli.Context) error {
					return httpPost(c, "/v1/system/backups", nil)
				},
			},
			{
				Name:  "purge",
				Usage: "Purge expired logs (state transitions, jobs, audit logs)",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "days", Value: 30, Usage: "Retention days cutoff"},
				},
				Action: func(c *cli.Context) error {
					payload := map[string]any{
						"days": c.Int("days"),
					}
					return httpPost(c, "/v1/system/purge", payload)
				},
			},
		},
	}
}

func maintenanceCommand() *cli.Command {
	return &cli.Command{
		Name:  "maintenance",
		Usage: "System maintenance and log retention operations",
		Subcommands: []*cli.Command{
			{
				Name:  "purge",
				Usage: "Purge expired logs (state transitions, jobs, audit logs)",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "days", Value: 30, Usage: "Retention days cutoff"},
				},
				Action: func(c *cli.Context) error {
					payload := map[string]any{
						"days": c.Int("days"),
					}
					return httpPost(c, "/v1/system/purge", payload)
				},
			},
		},
	}
}

func httpGet(c *cli.Context, path string) error {
	url := c.String("endpoint") + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return sendRequest(c, req)
}

func httpPost(c *cli.Context, path string, bodyData any) error {
	url := c.String("endpoint") + path
	var body io.Reader
	if bodyData != nil {
		b, err := json.Marshal(bodyData)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return err
	}
	if bodyData != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return sendRequest(c, req)
}

func httpDelete(c *cli.Context, path string) error {
	url := c.String("endpoint") + path
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	return sendRequest(c, req)
}

func sendRequest(c *cli.Context, req *http.Request) error {
	if token := c.String("token"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(data))
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP error %d", resp.StatusCode)
	}
	return nil
}
