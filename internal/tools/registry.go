package tools

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Param struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type Tool struct {
	Name        string  `json:"toolName"`
	Description string  `json:"description"`
	Method      string  `json:"method"`
	Path        string  `json:"upstreamPath"`
	Tier        string  `json:"tier"`
	DataKind    string  `json:"dataKind"`
	Cacheable   bool    `json:"cacheable"`
	Params      []Param `json:"params"`
}

type Registry struct {
	tools map[string]Tool
	list  []Tool
}

type PreparedRequest struct {
	Method string
	Path   string
	Query  url.Values
}

var pathParamRE = regexp.MustCompile(`:([A-Za-z][A-Za-z0-9_]*)`)

func NewRegistry(tools []Tool) *Registry {
	byName := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		if tool.Method == "" {
			tool.Method = http.MethodGet
		}
		byName[tool.Name] = tool
	}
	list := make([]Tool, 0, len(byName))
	for _, tool := range byName {
		list = append(list, tool)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return &Registry{tools: byName, list: list}
}

func (r *Registry) List() []Tool {
	out := make([]Tool, len(r.list))
	copy(out, r.list)
	return out
}

func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (t Tool) Prepare(arguments map[string]any) (PreparedRequest, error) {
	path := t.Path
	query := url.Values{}

	pathParams := map[string]Param{}
	queryParams := map[string]Param{}
	for _, param := range t.Params {
		switch param.In {
		case "path":
			pathParams[param.Name] = param
		case "query":
			queryParams[param.Name] = param
		}
	}

	for _, param := range pathParams {
		value, ok := arguments[param.Name]
		if !ok || value == nil || fmt.Sprint(value) == "" {
			return PreparedRequest{}, fmt.Errorf("missing required path parameter %q", param.Name)
		}
		path = strings.ReplaceAll(path, ":"+param.Name, url.PathEscape(valueString(value)))
	}

	for _, param := range t.Params {
		if !param.Required {
			continue
		}
		if _, isPath := pathParams[param.Name]; isPath {
			continue
		}
		value, ok := arguments[param.Name]
		if !ok || value == nil || fmt.Sprint(value) == "" {
			return PreparedRequest{}, fmt.Errorf("missing required query parameter %q", param.Name)
		}
	}

	for name, value := range arguments {
		if _, ok := pathParams[name]; ok {
			continue
		}
		if _, ok := queryParams[name]; !ok {
			return PreparedRequest{}, fmt.Errorf("unknown parameter %q", name)
		}
		if value == nil {
			continue
		}
		query.Set(name, valueString(value))
	}

	return PreparedRequest{Method: t.Method, Path: path, Query: query}, nil
}

func valueString(value any) string {
	switch v := value.(type) {
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	default:
		return fmt.Sprint(value)
	}
}

func pathParams(path string) []Param {
	matches := pathParamRE.FindAllStringSubmatch(path, -1)
	params := make([]Param, 0, len(matches))
	for _, match := range matches {
		params = append(params, Param{Name: match[1], In: "path", Type: "string", Required: true})
	}
	return params
}

func queryParams(required []string, optional []string) []Param {
	params := make([]Param, 0, len(required)+len(optional))
	for _, name := range required {
		params = append(params, Param{Name: name, In: "query", Type: inferType(name), Required: true})
	}
	for _, name := range optional {
		params = append(params, Param{Name: name, In: "query", Type: inferType(name), Required: false})
	}
	return params
}

func inferType(name string) string {
	switch strings.ToLower(name) {
	case "limit", "count", "offset", "pagesize", "index", "st", "type", "sorttype", "order", "filtergem", "filtertib", "filtermotherboard", "minamount", "threshold":
		return "number"
	default:
		return "string"
	}
}

func makeTool(name string, desc string, path string, tier string, dataKind string, required []string, optional []string) Tool {
	params := append(pathParams(path), queryParams(required, optional)...)
	return Tool{
		Name:        name,
		Description: desc,
		Method:      http.MethodGet,
		Path:        path,
		Tier:        tier,
		DataKind:    dataKind,
		Cacheable:   dataKind != "realtime",
		Params:      params,
	}
}

func DefaultRegistry() *Registry {
	common := []string{"date", "limit", "count"}
	stockRange := []string{"startDate", "endDate", "limit"}
	l2 := []string{"date", "time", "sortBy", "limit", "offset", "Type", "SortType", "Order", "FilterGem", "FilterTIB", "FilterMotherboard", "board", "fast", "response", "RStart", "REnd", "pageSize"}
	auction := []string{"date", "limit", "sortBy", "source", "minAmount", "threshold", "type"}
	auctionActiveSector := []string{"date", "limit", "sortBy", "source", "minAmount", "threshold", "type", "group", "list", "tab"}
	auctionActiveSectorStocks := []string{"date", "limit", "sortBy", "source", "filter"}
	gray := []string{"date", "tab", "pageSize", "sortBy", "order", "format", "limit"}

	defs := []Tool{
		makeTool("market.sentiment", "市场情绪总览", "/api/sentiment", "pro", "historical_or_realtime", nil, common),
		makeTool("market.mood", "市场情绪分布", "/api/emotion/mood", "pro", "historical_or_realtime", nil, common),
		makeTool("market.distribution", "涨跌分布", "/api/emotion/distribution", "pro", "historical_or_realtime", nil, common),
		makeTool("market.limit_up_performance", "涨停表现与连板梯队", "/api/limit-up-performance", "pro", "historical_or_realtime", nil, common),
		makeTool("market.ladder", "市场连板梯队", "/api/ladder", "pro", "historical_or_realtime", nil, common),
		makeTool("market.consecutive", "指定连板层级", "/api/consecutive/:level", "pro", "historical_or_realtime", nil, common),
		makeTool("market.daban", "打板模块指定标签", "/api/market/daban/:tab", "pro", "historical_or_realtime", nil, common),
		makeTool("market.broken", "炸板池兼容接口", "/api/broken", "pro", "historical_or_realtime", nil, common),
		makeTool("market.daban_broken", "打板炸板标签", "/api/market/daban/broken", "pro", "historical_or_realtime", nil, common),
		makeTool("market.daban_near_limit", "接近涨停标签", "/api/market/daban/near-limit", "pro", "historical_or_realtime", nil, common),
		makeTool("market.daban_wind_vane", "风向标标签", "/api/market/daban/wind-vane", "pro", "historical_or_realtime", nil, common),
		makeTool("market.daban_new_stock", "新股标签", "/api/market/daban/new-stock", "pro", "historical_or_realtime", nil, common),
		makeTool("market.withdrawal", "撤单相关数据", "/api/withdrawal", "pro", "historical_or_realtime", nil, common),
		makeTool("market.realtime_withdrawal", "实时撤单数据", "/api/realtime/withdrawal", "pro", "realtime", nil, common),
		makeTool("market.realtime_limitcount", "实时涨跌停统计", "/api/realtime/limitcount", "pro", "realtime", nil, common),
		makeTool("market.tail_rush", "尾盘抢筹排行", "/api/market/tail-rush", "inst", "historical_or_realtime", nil, common),
		makeTool("market.live_alerts", "盘中联动提醒", "/api/market/live-alerts", "inst", "realtime", nil, []string{"limit"}),
		makeTool("market.new_high", "百日新高", "/api/market/new-high", "pro", "historical_or_realtime", nil, common),
		makeTool("market.auction", "早盘/尾盘竞价异动排行", "/api/market/auction", "inst", "historical_or_realtime", nil, []string{"type", "date", "limit"}),
		makeTool("market.auction_rank", "竞价异动排行", "/api/market/auction/rank", "inst", "historical_or_realtime", nil, []string{"type", "date", "limit"}),
		makeTool("market.l2_rank", "全市场 L2 主力净额", "/api/market/l2-rank", "inst", "historical_or_realtime", nil, l2),
		makeTool("market.gray_market", "暗盘资金榜兼容接口", "/api/market/gray-market", "pro", "historical_or_realtime", nil, gray),
		makeTool("market.theme_events", "题材事件催化", "/api/market/theme-events", "pro", "historical_or_realtime", nil, common),
		makeTool("market_ladder", "市场梯队兼容接口", "/api/market-ladder", "pro", "historical_or_realtime", nil, common),
		makeTool("sector_ladder", "板块梯队兼容接口", "/api/sector-ladder", "pro", "historical_or_realtime", nil, common),

		makeTool("sector.ranking", "精选板块强度排行历史", "/api/sectors", "pro", "historical", []string{"date"}, []string{"limit", "count"}),
		makeTool("sector.ranking_alias", "精选板块强度排行历史别名", "/api/sector/ranking", "pro", "historical", []string{"date"}, []string{"limit", "count"}),
		makeTool("sector.board", "精选板块实时榜单", "/api/sector/board", "pro", "realtime", nil, []string{"limit", "count"}),
		makeTool("sector.board_rank", "精选板块实时榜单别名", "/api/sector/board-rank", "pro", "realtime", nil, []string{"limit", "count"}),
		makeTool("sector.strength", "单板块强度", "/api/sector/strength/:code", "pro", "historical_or_realtime", nil, common),
		makeTool("sector.capital", "单板块盘口资金", "/api/sector/capital/:code", "pro", "historical_or_realtime", nil, common),
		makeTool("sector.stocks", "板块成分股原始池", "/api/sector/stocks/:code", "pro", "historical_or_realtime", nil, []string{"date", "limit", "all"}),
		makeTool("sector.stocks_structured", "板块成分股结构化池", "/api/sector/stocks/:code/structured", "pro", "historical_or_realtime", nil, []string{"date", "limit", "all"}),
		makeTool("sector.news", "板块新闻催化", "/api/sector/news/:code", "pro", "realtime", nil, nil),
		makeTool("sector.intraday", "板块分时走势", "/api/intraday/sector/:code", "pro", "historical_or_realtime", nil, common),
		makeTool("sector.index", "板块通用兼容接口", "/api/sector", "pro", "historical_or_realtime", nil, common),
		makeTool("theme.list", "活跃题材列表", "/api/theme/list", "pro", "realtime", nil, nil),
		makeTool("theme.home_library", "首页题材库目录", "/api/home/theme-library", "pro", "realtime", nil, nil),
		makeTool("conception.point", "概念热点", "/api/conception/point", "pro", "realtime", nil, nil),
		makeTool("conception.history", "概念历史热度", "/api/conception/history", "pro", "historical", nil, common),
		makeTool("news.themes", "题材新闻流", "/api/news/themes", "pro", "realtime", nil, []string{"limit"}),
		makeTool("news.focus", "7x24 财经快讯", "/api/news/focus", "free", "realtime", nil, []string{"limit"}),
		makeTool("news.selected", "精选财经新闻", "/api/news/selected", "free", "realtime", nil, []string{"limit"}),
		makeTool("live.content", "盘中直播和异动提示", "/api/live/content", "free", "realtime", nil, nil),

		makeTool("stock.intraday", "个股分时", "/api/intraday/stock/:code", "pro", "historical_or_realtime", nil, common),
		makeTool("stock.intraday_legacy", "个股分时兼容路径", "/api/intraday/stock-legacy/:code", "pro", "historical_or_realtime", nil, common),
		makeTool("stock.auction_bid_daily", "全市场竞价分时 manifest", "/api/auction/stock-bid/daily", "inst", "historical_or_realtime", nil, common),
		makeTool("stock.auction_bid_daily_board", "全市场竞价分时板块包", "/api/auction/stock-bid/daily/:board", "inst", "historical_or_realtime", nil, []string{"date", "lite"}),
		makeTool("stock.auction_bid", "个股竞价分时", "/api/auction/stock-bid/:code", "inst", "realtime", nil, common),
		makeTool("stock.kline", "个股 K 线", "/api/stock/kline/:code", "free", "historical", nil, []string{"type", "begin", "end", "date"}),
		makeTool("stock.limit_up_reason", "个股涨停原因", "/api/stock/limit-up-reason/:code", "pro", "historical_or_realtime", nil, common),
		makeTool("stock.zt_reason", "个股涨停原因兼容路径", "/api/stock/zt-reason/:code", "pro", "historical_or_realtime", nil, common),
		makeTool("stock.limit_up_reason_history", "个股涨停原因历史列表", "/api/stock/limit-up-reason-history/:code", "pro", "historical", nil, common),
		makeTool("stock.ztgene", "个股涨停基因", "/api/stock/ztgene/:code", "pro", "historical", nil, common),
		makeTool("stock.bigorder", "单股 L2 主力净额", "/api/stock/bigorder/:code", "inst", "historical_or_realtime", nil, common),
		makeTool("stock.depth", "个股五档盘口", "/api/stock/depth/:code", "inst", "realtime", nil, nil),
		makeTool("block_trade", "大宗交易明细", "/api/block-trade", "pro", "historical_or_realtime", nil, common),
		makeTool("home.versatile", "首页综合数据", "/api/home/versatile", "pro", "realtime", nil, nil),

		makeTool("index.list", "大盘指数", "/api/index", "free", "realtime", nil, nil),
		makeTool("index.realtime", "指定指数实时行情", "/api/index/realtime/:code", "free", "realtime", nil, nil),
		makeTool("index.kline", "指数 K 线", "/api/index/kline/:code", "free", "historical", nil, []string{"type", "begin", "end", "date"}),
		makeTool("index.trend", "指数分时走势", "/api/index/trend/:code", "free", "historical_or_realtime", nil, common),
		makeTool("index.intraday", "指数盘中分时", "/api/intraday/index/:code", "free", "historical_or_realtime", nil, common),
		makeTool("index.depth", "指数 L2 深度", "/api/index/depth/:code", "inst", "realtime", nil, nil),
		makeTool("index.realtime_indexes", "指数池实时行情", "/api/realtime/indexes", "free", "realtime", nil, []string{"ids"}),
		makeTool("index.global", "全球指数", "/api/global/index", "free", "realtime", nil, nil),
		makeTool("style.indexes", "风格指数", "/api/style/indexes", "free", "realtime", nil, nil),
		makeTool("commodity.list", "商品列表", "/api/commodity/list", "free", "realtime", nil, nil),
		makeTool("intraday.volume", "分时量能", "/api/intraday/volume/:code", "pro", "historical_or_realtime", nil, common),

		makeTool("lhb.list", "龙虎榜上榜个股列表", "/api/lhb/list", "inst", "historical_or_realtime", nil, common),
		makeTool("lhb.detail", "龙虎榜席位买卖明细", "/api/lhb/detail/:code", "inst", "historical_or_realtime", nil, common),
		makeTool("lhb.realtime", "实时龙虎榜兼容接口", "/api/lhb/realtime", "inst", "historical_or_realtime", nil, l2),
		makeTool("youzi.trends", "游资席位动向", "/api/youzi/trends", "inst", "historical_or_realtime", nil, common),

		makeTool("auction.opening", "开盘竞价概览", "/api/auction/opening", "inst", "realtime", nil, common),
		makeTool("auction.limit_bid", "涨停委买额", "/api/auction/limit-bid", "inst", "historical_or_realtime", nil, auction),
		makeTool("auction.morning_bidding", "涨停委买额兼容路径", "/api/auction/morning-bidding", "inst", "historical_or_realtime", nil, auction),
		makeTool("auction.matched_deals", "竞价成交额大于 2000W", "/api/auction/matched-deals", "inst", "historical_or_realtime", nil, auction),
		makeTool("auction.hot_stocks", "竞价人气股", "/api/auction/hot-stocks", "inst", "historical_or_realtime", nil, auction),
		makeTool("auction.main_net", "竞价净额主力净额", "/api/auction/main-net", "inst", "historical", []string{"date"}, auction),
		makeTool("auction.auction_net", "竞价净额主力净额兼容路径", "/api/auction/auction-net", "inst", "historical", []string{"date"}, auction),
		makeTool("auction.sell_pressure", "竞价砸盘", "/api/auction/sell-pressure", "inst", "historical_or_realtime", nil, auction),
		makeTool("auction.dump_stocks", "竞价砸盘兼容路径", "/api/auction/dump-stocks", "inst", "historical_or_realtime", nil, auction),
		makeTool("auction.active_sectors", "板块竞价异动分组榜", "/api/auction/active-sectors", "inst", "historical_or_realtime", nil, auctionActiveSector),
		makeTool("auction.active_sector_stocks", "板块竞价异动个股明细", "/api/auction/active-sectors/:sectorId/stocks", "inst", "historical_or_realtime", nil, auctionActiveSectorStocks),
		makeTool("auction.sector_stocks", "板块竞价异动个股明细兼容路径", "/api/auction/sector-stocks/:sectorId", "inst", "historical_or_realtime", nil, auctionActiveSectorStocks),
		makeTool("auction.active_stocks", "竞价活跃个股", "/api/auction/active-stocks", "inst", "realtime", nil, common),
		makeTool("auction.dashboard", "竞价全景看板", "/api/auction/dashboard", "inst", "historical_or_realtime", nil, common),

		makeTool("hot.rank", "人气榜和最强风口榜单", "/api/hot/rank", "pro", "historical_or_realtime", nil, common),
		makeTool("gray_market.eastmoney", "东方财富暗盘资金榜", "/api/eastmoney/gray-market", "pro", "historical_or_realtime", nil, gray),
		makeTool("gray_market.darktrade", "东方财富暗盘资金榜兼容路径", "/api/eastmoney/darktrade", "pro", "historical_or_realtime", nil, gray),

		makeTool("topic.tomorrow", "明天炒什么列表", "/api/topic/tomorrow", "pro", "historical", nil, []string{"index", "Index", "st", "limit"}),
		makeTool("topic.tomorrow_alias", "明天炒什么列表兼容路径", "/api/tomorrow/topics", "pro", "historical", nil, []string{"index", "Index", "st", "limit"}),
		makeTool("topic.tomorrow_detail", "明天炒什么详情", "/api/topic/tomorrow/:id", "pro", "historical", nil, nil),
		makeTool("topic.tomorrow_stocks", "明天炒什么利好个股", "/api/topic/tomorrow/:id/stocks", "pro", "historical", []string{"sector"}, nil),

		makeTool("shareholder.changes", "股东人数变动", "/api/shareholder/changes", "pro", "historical", nil, []string{"startDate", "endDate", "limit"}),
		makeTool("shareholder.track_by_holder", "按机构追踪股东持股", "/api/shareholder/track/by-holder", "pro", "realtime", []string{"holderId"}, []string{"limit"}),
		makeTool("shareholder.track_by_stock", "按个股追踪机构股东", "/api/shareholder/track/by-stock", "pro", "historical_or_realtime", []string{"code"}, []string{"date", "limit"}),
		makeTool("financial.institution_holdings", "机构增仓最新季度", "/api/financial/institution-holdings", "pro", "realtime", nil, []string{"limit", "type", "order"}),
		makeTool("financial.institution_holdings_history", "机构增仓历史季度", "/api/financial/institution-holdings/history", "pro", "historical", []string{"date"}, []string{"limit"}),
		makeTool("stats.sector_range", "板块区间统计", "/api/stats/sector-range", "pro", "historical", []string{"startDate", "endDate"}, stockRange),
		makeTool("stats.stock_range", "全市场个股区间统计", "/api/stats/stock-range", "pro", "historical", []string{"startDate", "endDate"}, stockRange),

		makeTool("monitor.regulatory_focus", "重点监控股票", "/api/monitor/regulatory/focus", "inst", "realtime", nil, nil),
		makeTool("monitor.focus_stocks", "重点监控股票兼容路径", "/api/monitor/focus-stocks", "inst", "realtime", nil, nil),
		makeTool("monitor.regulatory_repeated", "多次异动个股", "/api/monitor/regulatory/repeated", "inst", "realtime", nil, []string{"limit"}),
		makeTool("monitor.repeated_anomaly", "多次异动个股兼容路径", "/api/monitor/repeated-anomaly", "inst", "realtime", nil, []string{"limit"}),

		makeTool("history.strength", "历史强度", "/api/history/strength", "pro", "historical", nil, common),
		makeTool("history.analysis", "历史分析", "/api/history/analysis", "pro", "historical", nil, common),
		makeTool("history.zdnum", "历史涨跌数量", "/api/history/zdnum", "pro", "historical", nil, common),
		makeTool("realtime.board", "实时板块类型", "/api/realtime/board/:type", "pro", "realtime", nil, []string{"limit"}),
		makeTool("mood", "市场情绪兼容接口", "/api/mood", "pro", "historical_or_realtime", nil, common),
	}

	return NewRegistry(defs)
}
