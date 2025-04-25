package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/urfave/cli/v2"
)

var cmdRelayAdmin = &cli.Command{
	Name:  "admin",
	Usage: "sub-comands for relay administration",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "admin-password",
			Usage:   "relay admin password (for Basic admin auth)",
			EnvVars: []string{"RELAY_ADMIN_PASSWORD", "ATP_AUTH_ADMIN_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "admin-bearer-token",
			Usage:   "relay admin auth token (for Bearer auth)",
			EnvVars: []string{"RELAY_ADMIN_BEARER_TOKEN"},
		},
	},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:  "account",
			Usage: "sub-commands for managing accounts",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:  "takedown",
					Usage: "takedown a single account on relay",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:    "collection",
							Aliases: []string{"c"},
							Usage:   "collection (NSID) to match",
						},
						&cli.BoolFlag{
							Name:  "reverse",
							Usage: "un-takedown",
						},
					},
					Action: runRelayAdminAccountTakedown,
				},
				&cli.Command{
					Name:    "list",
					Aliases: []string{"ls"},
					Usage:   "enumerate accounts (eg, takendown)",
					Action:  runRelayAdminAccountList,
				},
			},
		},
		&cli.Command{
			Name:  "host",
			Usage: "sub-commands for upstream hosts (eg, PDS)",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "add",
					Usage:     "request crawl of upstream host (eg, PDS)",
					ArgsUsage: `<hostname>`,
					Action:    runRelayAdminHostAdd,
				},
				&cli.Command{
					Name:      "block",
					Usage:     "request crawl of upstream host (eg, PDS)",
					ArgsUsage: `<hostname>`,
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:  "reverse",
							Usage: "un-takedown",
						},
					},
					Action: runRelayAdminHostBlock,
				},
				&cli.Command{
					Name:    "list",
					Aliases: []string{"ls"},
					Usage:   "enumerate hosts crawled by relay",
					Action:  runRelayAdminHostList,
				},
				&cli.Command{
					Name:      "config",
					Usage:     "update rate-limits per host",
					ArgsUsage: `<hostname>`,
					Flags: []cli.Flag{
						&cli.IntFlag{
							Name: "account-limit",
						},
					},
					Action: runRelayAdminHostConfig,
				},
			},
		},
		&cli.Command{
			Name:  "domain",
			Usage: "sub-commands for domain-level config",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "ban",
					Usage:     "ban an entire domain name from being crawled",
					ArgsUsage: `<domain>`,
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:  "reverse",
							Usage: "un-takedown",
						},
					},
					Action: runRelayAdminDomainBan,
				},
				&cli.Command{
					Name:    "list",
					Aliases: []string{"ls"},
					Usage:   "enumerate domains with configs (eg, bans)",
					Action:  runRelayAdminDomainList,
				},
			},
		},
		&cli.Command{
			Name:  "consumer",
			Usage: "sub-commands for consumers",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:    "list",
					Aliases: []string{"ls"},
					Usage:   "enumerate consumers",
					Action:  runRelayAdminConsumerList,
				},
			},
		},
	},
}

type RelayAdminClient struct {
	Host        string
	Password    string
	BearerToken string
}

func (c *RelayAdminClient) Do(method, path string, params map[string]string, body map[string]any) ([]byte, error) {
	u, err := url.Parse(c.Host)
	if err != nil {
		return nil, err
	}
	u.Path = path
	q := u.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	u.RawQuery = q.Encode()

	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(b)
	}

	var req *http.Request
	if buf != nil {
		req, err = http.NewRequest(method, u.String(), buf)
	} else {
		req, err = http.NewRequest(method, u.String(), nil)
	}
	if err != nil {
		return nil, err
	}
	if c.Password != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:"+c.Password)))
	} else if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	req.Header.Set("User-Agent", *userAgent())
	if buf != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		slog.Warn("relay HTTP error", "statusCode", resp.StatusCode, "body", string(respBytes))
		return nil, fmt.Errorf("relay HTTP request failed: %d", resp.StatusCode)
	}
	return respBytes, nil
}

func NewRelayAdminClient(cctx *cli.Context) (*RelayAdminClient, error) {
	client := RelayAdminClient{
		Host:        cctx.String("relay-host"),
		Password:    cctx.String("admin-password"),
		BearerToken: cctx.String("admin-bearer-token"),
	}
	if client.Password == "" && client.BearerToken == "" {
		return nil, fmt.Errorf("either admin password or admin bearer token must be provided")
	}
	return &client, nil
}

func runRelayAdminAccountTakedown(cctx *cli.Context) error {
	ctx := cctx.Context

	username := cctx.Args().First()
	if username == "" {
		return fmt.Errorf("need to provide username as an argument")
	}
	ident, err := resolveIdent(ctx, username)
	if err != nil {
		return err
	}

	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}

	path := "/admin/repo/takeDown"
	if cctx.Bool("reverse") {
		path = "/admin/repo/reverseTakedown"
	}

	body := map[string]any{
		"did": ident.DID.String(),
	}
	_, err = client.Do("POST", path, nil, body)
	if err != nil {
		return err
	}
	return nil
}

func runRelayAdminAccountList(cctx *cli.Context) error {
	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}
	path := "/admin/repo/takedowns"
	params := map[string]string{
		"cursor": "",
		"size":   "500",
	}
	for {
		respBytes, err := client.Do("GET", path, params, nil)
		if err != nil {
			return err
		}
		var resp map[string]any
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			return err
		}
		for _, d := range resp["dids"].([]any) {
			fmt.Println(d)
		}
		cursor, ok := resp["cursor"]
		if !ok || cursor == "" {
			break
		}
		params["cursor"] = cursor.(string)
	}
	return nil
}

func runRelayAdminHostAdd(cctx *cli.Context) error {

	hostname := cctx.Args().First()
	if hostname == "" {
		return fmt.Errorf("need to provide hostname as an argument")
	}

	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}
	path := "/admin/pds/requestCrawl"
	body := map[string]any{
		"hostname": hostname,
	}
	_, err = client.Do("POST", path, nil, body)
	if err != nil {
		return err
	}
	return nil
}

func runRelayAdminHostBlock(cctx *cli.Context) error {

	hostname := cctx.Args().First()
	if hostname == "" {
		return fmt.Errorf("need to provide hostname as an argument")
	}

	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}

	path := "/admin/pds/block"
	if cctx.Bool("reverse") {
		path = "/admin/pds/unblock"
	}

	params := map[string]string{
		"host": hostname,
	}
	_, err = client.Do("POST", path, params, nil)
	if err != nil {
		return err
	}
	return nil
}

func runRelayAdminHostList(cctx *cli.Context) error {
	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}
	path := "/admin/pds/list"

	respBytes, err := client.Do("GET", path, nil, nil)
	if err != nil {
		return err
	}
	var rows []map[string]any
	if err := json.Unmarshal(respBytes, &rows); err != nil {
		return err
	}
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			return nil
		}
		fmt.Println(string(b))
	}
	return nil
}

func runRelayAdminHostConfig(cctx *cli.Context) error {

	hostname := cctx.Args().First()
	if hostname == "" {
		return fmt.Errorf("need to provide hostname as an argument")
	}

	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}

	path := "/admin/pds/changeLimits"

	body := map[string]any{
		"host": hostname,
	}
	if cctx.IsSet("account-limit") {
		body["repo_limit"] = cctx.Int("account-limit")
	}

	_, err = client.Do("POST", path, nil, body)
	if err != nil {
		return err
	}
	return nil
}

func runRelayAdminDomainBan(cctx *cli.Context) error {

	domain := cctx.Args().First()
	if domain == "" {
		return fmt.Errorf("need to provide domain as an argument")
	}

	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}

	path := "/admin/subs/banDomain"
	if cctx.Bool("reverse") {
		path = "/admin/subs/unbanDomain"
	}

	body := map[string]any{
		"domain": domain,
	}
	_, err = client.Do("POST", path, nil, body)
	if err != nil {
		return err
	}
	return nil
}

func runRelayAdminDomainList(cctx *cli.Context) error {
	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}
	path := "/admin/subs/listDomainBans"

	respBytes, err := client.Do("GET", path, nil, nil)
	if err != nil {
		return err
	}
	var resp map[string]any
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return err
	}
	for _, d := range resp["banned_domains"].([]any) {
		fmt.Println(d)
	}
	return nil
}

func runRelayAdminConsumerList(cctx *cli.Context) error {
	client, err := NewRelayAdminClient(cctx)
	if err != nil {
		return err
	}
	path := "/admin/consumers/list"

	respBytes, err := client.Do("GET", path, nil, nil)
	if err != nil {
		return err
	}
	var rows []map[string]any
	if err := json.Unmarshal(respBytes, &rows); err != nil {
		return err
	}
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			return nil
		}
		fmt.Println(string(b))
	}
	return nil
}
