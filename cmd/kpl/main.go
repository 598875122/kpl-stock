package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"stock-kpl/pkg/kpllocal"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "kpl: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}

	switch args[0] {
	case "tools":
		return toolsCmd(args[1:])
	case "call":
		return callCmd(args[1:])
	case "path":
		return pathCmd(args[1:])
	case "bigorder":
		return manyCodeCmd("stock.bigorder", args[1:])
	case "intraday":
		return manyCodeCmd("stock.intraday", args[1:])
	case "auction-bid":
		return manyCodeCmd("stock.auction_bid", args[1:])
	case "auction-rank":
		return callToolCmd("market.auction_rank", args[1:])
	case "auction-limit-bid":
		return callToolCmd("auction.limit_bid", args[1:])
	case "auction-main-net":
		return callToolCmd("auction.main_net", args[1:])
	case "auction-dashboard":
		return callToolCmd("auction.dashboard", args[1:])
	case "active-sectors":
		return callToolCmd("auction.active_sectors", args[1:])
	case "market-l2-rank":
		return callToolCmd("market.l2_rank", args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func toolsCmd(args []string) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()
	return printJSON(client.Tools())
}

func callCmd(args []string) error {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	tool := fs.String("tool", "", "tool name")
	argJSON := fs.String("args", "{}", "JSON object arguments")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool is required")
	}
	parsedArgs, err := parseArgs(*argJSON, fs.Args())
	if err != nil {
		return err
	}
	return callTool(*tool, parsedArgs)
}

func pathCmd(args []string) error {
	fs := flag.NewFlagSet("path", flag.ContinueOnError)
	path := fs.String("path", "", "upstream path; may include query string")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("--path is required")
	}
	cleanPath, query, err := parseURLPath(*path)
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, err := client.CallPath(ctx, cleanPath, query)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func manyCodeCmd(tool string, args []string) error {
	fs := flag.NewFlagSet(tool, flag.ContinueOnError)
	codesRaw := fs.String("codes", "", "comma separated stock codes")
	workers := fs.Int("workers", kpllocal.DefaultWorkers, "parallel workers")
	argJSON := fs.String("args", "{}", "JSON object arguments")
	if err := fs.Parse(args); err != nil {
		return err
	}
	codes := splitList(*codesRaw)
	if len(codes) == 0 {
		return fmt.Errorf("--codes is required")
	}
	parsedArgs, err := parseArgs(*argJSON, fs.Args())
	if err != nil {
		return err
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	results := client.CallMany(ctx, tool, codes, parsedArgs, *workers)
	sort.SliceStable(results, func(i, j int) bool {
		return fmt.Sprint(results[i].Arguments["code"]) < fmt.Sprint(results[j].Arguments["code"])
	})
	return printJSON(results)
}

func callToolCmd(tool string, args []string) error {
	fs := flag.NewFlagSet(tool, flag.ContinueOnError)
	argJSON := fs.String("args", "{}", "JSON object arguments")
	if err := fs.Parse(args); err != nil {
		return err
	}
	parsedArgs, err := parseArgs(*argJSON, fs.Args())
	if err != nil {
		return err
	}
	return callTool(tool, parsedArgs)
}

func callTool(tool string, args map[string]any) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := client.Call(ctx, tool, args)
	if err != nil {
		return err
	}
	return printJSON(result)
}

func newClient() (*kpllocal.Client, error) {
	cfg, err := kpllocal.LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return kpllocal.New(cfg)
}

func parseArgs(jsonText string, pairs []string) (map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(jsonText) != "" {
		if err := json.Unmarshal([]byte(jsonText), &out); err != nil {
			return nil, fmt.Errorf("parse --args: %w", err)
		}
	}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("argument %q must be key=value", pair)
		}
		out[key] = value
	}
	return out, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func parseURLPath(rawPath string) (string, url.Values, error) {
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	parsed, err := url.Parse(rawPath)
	if err != nil {
		return "", nil, err
	}
	cleanPath := parsed.Path
	if cleanPath == "" {
		cleanPath = "/"
	}
	return cleanPath, parsed.Query(), nil
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  kpl tools
  kpl call --tool stock.bigorder --args '{"code":"600183","date":"20260612"}'
  kpl bigorder --codes 600183,600584 --args '{"date":"20260612"}' --workers 8
  kpl intraday --codes 600183,600584 --workers 8
  kpl path --path '/api/auction/limit-bid?limit=500'

Environment:
  KPL_API_KEY             required
  KPL_BASE_URL            default http://124.222.49.67:3000
  KPL_TIMEOUT_SECONDS     default 10
  KPL_CACHE_PATH          default /root/kpl-stock/data/kpl-cache.sqlite`)
}
