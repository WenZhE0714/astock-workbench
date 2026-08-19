package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/analysis"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type researchAgentRole struct {
	ID      string
	Label   string
	Mission string
}

var stockReportAgentRoles = []researchAgentRole{
	{ID: "technical", Label: "技术与量价", Mission: "只审计未复权日K、均线、MACD、RSI、量比、结构位和当日涨跌停边界，区分短线与波段"},
	{ID: "capital-sector", Label: "资金与板块", Mission: "只审计当日累计资金、1/3/5分钟增量、关联板块涨跌/资金/广度/排名及个股板块共振"},
	{ID: "event-risk", Label: "事件与风险", Mission: "只审计公告索引、新闻线索、龙虎榜、数据缺失和A股T+1/涨跌停风险，不把标题当已核实事实"},
	{ID: "bear-auditor", Label: "反方审计", Mission: "专门反驳过度乐观结论，检查证据冲突、数据时效、追高风险、越界价位和无法证实的因果"},
}

var aiChatAgentRoles = stockReportAgentRoles[:3]

var marketReportAgentRoles = []researchAgentRole{
	{ID: "index-liquidity", Label: "指数量能与承接", Mission: "审计指数趋势、市场成交额、量比、收盘位置、资金和上涨下跌结构，区分短线与中期"},
	{ID: "sector-rotation", Label: "板块轮动", Mission: "审计强弱板块涨跌、主力净额、广度、排名与龙头，识别共振、分歧和退潮"},
	{ID: "candidate-selection", Label: "候选股筛选", Mission: "审计候选股量价、流动性、板块地位、资金可用性、公告线索和分级理由，不扩充股票池"},
	{ID: "risk-auditor", Label: "风险与反方审计", Mission: "检查过拟合、数据缺失、资金不可用、静态截面、公告未核验和情绪退潮风险，反驳无依据的看多结论"},
}

func boundedAgentText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func researchAgentPrompt(role researchAgentRole, subject, freshness string, factsJSON []byte) string {
	return fmt.Sprintf(`你是由主Agent监督的A股%s子Agent，角色：%s。
任务：%s。

只能分析下方同一时点冻结的结构化事实。不要运行命令、读取文件、搜索网络或调用交易接口；不得补充JSON中没有的数字、财务、政策、公司业务或新闻事实，不得创造新价位。涉及外部事件或观点时必须引用evidence中已有的[E##]，禁止创造ID；A层只有verified_body=true才可作为事实，B层只作专业观点，C/D层只作线索或情绪，均不得单独支撑买卖结论。缺失值和warnings必须降低置信度；清洗后的0不能自动解释为持平。价位必须沿用事实并遵守price_boundary。

用中文Markdown控制在500字内，固定输出：
1. 数据时效；2. 核心证据；3. 偏多证据；4. 偏空/反证；5. 条件与失效；6. 置信度（高/中/低）及原因。
不要给无条件买卖指令，不要输出最终综合结论。

确定性时效摘要：%s
结构化事实JSON（冻结快照）：
%s`, subject, role.Label, role.Mission, freshness, factsJSON)
}

func (app *App) runResearchAgents(
	ctx context.Context,
	subject, freshness string,
	facts any,
	roles []researchAgentRole,
	progress func(string),
) []domain.AgentResearchRun {
	runs := make([]domain.AgentResearchRun, len(roles))
	if app.marketReportAI == nil {
		for index, role := range roles {
			runs[index] = domain.AgentResearchRun{Role: role.ID, Label: role.Label, Status: "unavailable", Error: "Codex Agent 未初始化"}
		}
		return runs
	}
	payload, err := json.Marshal(facts)
	if err != nil {
		for index, role := range roles {
			runs[index] = domain.AgentResearchRun{Role: role.ID, Label: role.Label, Status: "failed", Error: err.Error()}
		}
		return runs
	}
	factsHash := declaredSnapshotHash(facts)
	if factsHash == "" {
		factsHash = snapshotHash(payload)
	}
	if progress != nil {
		progress(fmt.Sprintf("Codex %d个专业Agent并行分析同一数据快照", len(roles)))
	}
	var wait sync.WaitGroup
	var nonCodexMu sync.Mutex
	for index, role := range roles {
		index, role := index, role
		wait.Add(1)
		go func() {
			defer wait.Done()
			startedAt := time.Now()
			run := domain.AgentResearchRun{
				Role: role.ID, Label: role.Label, Status: "running", StartedAt: startedAt, FactsHash: factsHash,
			}
			synthesizer := app.marketReportAI
			if _, ok := synthesizer.(*analysis.CodexRunner); ok {
				temporary, tempError := os.MkdirTemp("", "astock-research-agent-*")
				if tempError != nil {
					run.Status, run.Error = "failed", tempError.Error()
					run.FinishedAt, run.Duration = time.Now(), time.Since(startedAt)
					runs[index] = run
					return
				}
				defer os.RemoveAll(temporary)
				synthesizer = analysis.NewCodexRunner(temporary)
			} else {
				nonCodexMu.Lock()
				defer nonCodexMu.Unlock()
			}
			callContext, cancel := context.WithTimeout(ctx, codexReportTimeout())
			text, callError := synthesizer.Synthesize(callContext, researchAgentPrompt(role, subject, freshness, payload))
			cancel()
			run.FinishedAt, run.Duration = time.Now(), time.Since(startedAt)
			if callError != nil {
				run.Status, run.Error = "failed", callError.Error()
			} else if strings.TrimSpace(text) == "" {
				run.Status, run.Error = "failed", "Agent未返回分析"
			} else {
				run.Status, run.Analysis = "ok", boundedAgentText(text, 3000)
			}
			runs[index] = run
		}()
	}
	wait.Wait()
	return runs
}

func snapshotHash(payload []byte) string {
	value := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", value[:])
}

func declaredSnapshotHash(value any) string {
	switch facts := value.(type) {
	case domain.StockReportFacts:
		return facts.SnapshotHash
	case *domain.StockReportFacts:
		return facts.SnapshotHash
	case domain.MarketScanFacts:
		return facts.SnapshotHash
	case *domain.MarketScanFacts:
		return facts.SnapshotHash
	default:
		return ""
	}
}

func setSnapshotHash(value any) string {
	switch facts := value.(type) {
	case *domain.StockReportFacts:
		facts.SnapshotHash = ""
		payload, err := json.Marshal(facts)
		if err != nil {
			return ""
		}
		facts.SnapshotHash = snapshotHash(payload)
		return facts.SnapshotHash
	case *domain.MarketScanFacts:
		facts.SnapshotHash = ""
		payload, err := json.Marshal(facts)
		if err != nil {
			return ""
		}
		facts.SnapshotHash = snapshotHash(payload)
		return facts.SnapshotHash
	default:
		return ""
	}
}

func successfulAgentCount(runs []domain.AgentResearchRun) int {
	count := 0
	for _, run := range runs {
		if run.Status == "ok" {
			count++
		}
	}
	return count
}

func researchFailureSummary(runs []domain.AgentResearchRun) string {
	failed := make([]string, 0)
	for _, run := range runs {
		if run.Status != "ok" {
			reason := boundedAgentText(run.Error, 120)
			if reason == "" {
				reason = run.Status
			}
			failed = append(failed, run.Label+": "+reason)
		}
	}
	return strings.Join(failed, "；")
}

func agentEvidenceJSON(runs []domain.AgentResearchRun) string {
	data, err := json.Marshal(runs)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func researchSupervisorPrompt(basePrompt, subject, freshness string, runs []domain.AgentResearchRun) string {
	return fmt.Sprintf(`你是A股%s多角色研究的主Agent。下面的结构化事实是唯一数据底盘，下面的角色意见只是独立审计证据。

综合规则：
1. 事实JSON优先于任何角色意见；角色意见不得引入事实JSON没有的数字或事件。
2. 数据采集时间、行情时间、日K截止日、市场状态、警告数和成功角色数会由系统统一放在报告顶部；正文不要重复，只需让过期或缺失数据影响结论。
3. 总结角色共识与分歧；失败角色不能被当作支持或反对证据。
4. 结论必须是条件式的，分别写短线/波段或盘面/中期方向、触发条件、失效条件、风险和置信度；禁止无条件买卖指令。
5. 缺失字段、过期日K、未核验公告/新闻、资金不可用都要降低置信度并明确标注。
6. 外部事件和专业观点只能引用冻结evidence中的[E##]；禁止创造凭证ID。只有official_disclosure且verified_body=true的A层可作公告事实；disclosure_index只是第三方公告索引，B层只作观点，其他C/D层只作背景或情绪，不能单独推出买卖结论。
7. 最终正文不要重复顶部已有的数据时效与Agent成功数，不输出JSON字段名；技术操作条件优先使用靠近现价的均线位，远端20日极值仅作二级结构确认；龙虎榜必须带日期并明确为历史异动日席位统计。

确定性时效摘要：%s
独立角色结果JSON：
%s

请基于下方原始编辑要求输出最终中文Markdown：
%s`, subject, freshness, agentEvidenceJSON(runs), basePrompt)
}

func sortedWarnings(warnings []string) []string {
	result := append([]string(nil), warnings...)
	sort.Strings(result)
	return result
}

func parseFactDate(value string) (time.Time, bool) {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), shanghaiLocation)
	return parsed, err == nil
}

func previousTradingDate(now time.Time) time.Time {
	day := now.In(shanghaiLocation).AddDate(0, 0, -1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

func completedDailyBars(bars []domain.DailyBar, generatedAt time.Time) ([]domain.DailyBar, string) {
	if len(bars) == 0 || strings.TrimSpace(bars[len(bars)-1].Date) == "" {
		return bars, ""
	}
	latest := bars[len(bars)-1]
	latestDate, ok := parseFactDate(latest.Date)
	if !ok {
		return bars, "日K日期格式无效，无法确认最新一根是否完整"
	}
	local := generatedAt.In(shanghaiLocation)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
	if latestDate.Equal(today) && marketSessionAt(generatedAt).Label != "已收盘" {
		return append([]domain.DailyBar(nil), bars[:len(bars)-1]...), "最新日K为当日未完成柱，技术分析已退回上一完整交易日"
	}
	return bars, ""
}

func dailyFreshnessWarning(dataDate, source string, generatedAt time.Time) string {
	date, ok := parseFactDate(dataDate)
	if !ok {
		return "日K截止日期缺失或无效，技术结论置信度降低"
	}
	expected := previousTradingDate(generatedAt)
	if marketSessionAt(generatedAt).Label == "已收盘" {
		local := generatedAt.In(shanghaiLocation)
		expected = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
	}
	ageDays := int(expected.Sub(date).Hours() / 24)
	if ageDays > 4 || (ageDays > 1 && strings.Contains(source, "缓存")) {
		return fmt.Sprintf("日K截至%s，较应有交易日明显滞后，技术结论仅作低置信度参考", dataDate)
	}
	if strings.Contains(source, "缓存") {
		return fmt.Sprintf("在线日K不可用，使用本地缓存（日K截至%s），结论需结合最新行情复核", dataDate)
	}
	return ""
}

func quoteFreshnessWarning(quoteTime string, generatedAt time.Time) string {
	if !marketSessionAt(generatedAt).Poll {
		return ""
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(quoteTime), shanghaiLocation)
	if err != nil {
		return "交易时段行情时间缺失或无效，实时结论置信度降低"
	}
	age := generatedAt.In(shanghaiLocation).Sub(parsed)
	if age > 3*time.Minute || age < -time.Minute {
		return fmt.Sprintf("行情时间为%s，与采集时点相差%.0f分钟，实时结论需复核", quoteTime, math.Abs(age.Minutes()))
	}
	return ""
}

func stockFactsFreshness(facts domain.StockReportFacts) string {
	quoteTime := strings.TrimSpace(facts.Quote.QuoteTime)
	if quoteTime == "" {
		quoteTime = "实时行情时间缺失"
	}
	dailyDate := strings.TrimSpace(facts.Technical.DataDate)
	if dailyDate == "" {
		dailyDate = "日K日期缺失"
	}
	return fmt.Sprintf("采集%s；行情%s；未复权日K截至%s；市场%s；警告%d项",
		facts.GeneratedAt.Format("2006-01-02 15:04:05"), quoteTime, dailyDate,
		marketSessionAt(facts.GeneratedAt).Label, len(facts.Warnings))
}

func marketFactsFreshness(facts domain.MarketScanFacts) string {
	dates := make([]string, 0, len(facts.Indices))
	for _, index := range facts.Indices {
		if index.Technical.DataDate != "" && !strings.Contains(strings.Join(dates, ","), index.Technical.DataDate) {
			dates = append(dates, index.Technical.DataDate)
		}
	}
	dailyDates := strings.Join(dates, ",")
	if dailyDates == "" {
		dailyDates = "缺失"
	}
	quoteTime := facts.QuoteTime
	if quoteTime == "" {
		quoteTime = "缺失"
	}
	return fmt.Sprintf("采集%s；行情%s；市场%s；指数日K截至%s；警告%d项",
		facts.GeneratedAt.Format("2006-01-02 15:04:05"), quoteTime, facts.MarketStatus, dailyDates, len(facts.Warnings))
}

func attachResearchFreshness(markdown, freshness string, runs []domain.AgentResearchRun) string {
	if strings.Contains(markdown, "数据时效") {
		return strings.TrimSpace(markdown)
	}
	return fmt.Sprintf("> 数据时效：%s；多角色Agent成功%d/%d。\n\n%s",
		freshness, successfulAgentCount(runs), len(runs), strings.TrimSpace(markdown))
}
