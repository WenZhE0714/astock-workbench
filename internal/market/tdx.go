package market

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

//go:embed tdx_bridge.py
var tdxBridgeSource string

const (
	tdxMetadataTTL   = 30 * time.Minute
	tdxRetryBackoff  = 15 * time.Second
	tdxDefaultPython = "python3"
)

// TDXOptions configures the optional tdxrs-backed TCP adapter. The adapter is
// deliberately opt-in; the normal HTTP data path remains dependency-free.
type TDXOptions struct {
	Python      string
	Server      string
	MetadataTTL time.Duration
}

type tdxSession struct {
	mu     sync.Mutex
	python string
	server string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr bytes.Buffer
	script string
	nextID int
}

type tdxEnvelope struct {
	ID     int             `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
}

func newTDXSession(options TDXOptions) *tdxSession {
	python := strings.TrimSpace(options.Python)
	if python == "" {
		python = strings.TrimSpace(os.Getenv("ASTOCK_TDX_PYTHON"))
	}
	if python == "" {
		python = tdxDefaultPython
	}
	return &tdxSession{python: python, server: strings.TrimSpace(options.Server)}
}

func readLineContext(ctx context.Context, reader *bufio.Reader) ([]byte, error) {
	type result struct {
		line []byte
		err  error
	}
	channel := make(chan result, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		channel <- result{line: line, err: err}
	}()
	select {
	case value := <-channel:
		return value.line, value.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (session *tdxSession) startLocked(ctx context.Context) error {
	if session.cmd != nil {
		return nil
	}
	temporary, err := os.CreateTemp("", "astock-tdx-bridge-*.py")
	if err != nil {
		return err
	}
	session.script = temporary.Name()
	if _, err := temporary.WriteString(tdxBridgeSource); err != nil {
		_ = temporary.Close()
		_ = os.Remove(session.script)
		session.script = ""
		return err
	}
	if err := temporary.Chmod(0o700); err != nil {
		_ = temporary.Close()
		_ = os.Remove(session.script)
		session.script = ""
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(session.script)
		session.script = ""
		return err
	}
	arguments := []string{session.script}
	if session.server != "" {
		arguments = append(arguments, "--server", session.server)
	}
	command := exec.Command(session.python, arguments...)
	session.stderr.Reset()
	stdin, err := command.StdinPipe()
	if err != nil {
		_ = os.Remove(session.script)
		session.script = ""
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = os.Remove(session.script)
		session.script = ""
		return err
	}
	command.Stderr = &session.stderr
	if err := command.Start(); err != nil {
		_ = os.Remove(session.script)
		session.script = ""
		return fmt.Errorf("启动 TDX Python 桥接失败: %w", err)
	}
	session.cmd = command
	session.stdin = stdin
	session.stdout = bufio.NewReaderSize(stdout, 1<<20)
	session.nextID = 1
	line, err := readLineContext(ctx, session.stdout)
	if err != nil {
		session.resetLocked()
		return fmt.Errorf("TDX 桥接启动超时: %w", err)
	}
	var handshake tdxEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(line), &handshake); err != nil {
		session.resetLocked()
		return fmt.Errorf("TDX 桥接握手无效: %w", err)
	}
	if !handshake.OK {
		session.resetLocked()
		return fmt.Errorf("TDX 桥接连接失败: %s", strings.TrimSpace(handshake.Error))
	}
	return nil
}

func (session *tdxSession) resetLocked() {
	if session.stdin != nil {
		_ = session.stdin.Close()
	}
	if session.cmd != nil && session.cmd.Process != nil {
		_ = session.cmd.Process.Kill()
		_ = session.cmd.Wait()
	}
	if session.script != "" {
		_ = os.Remove(session.script)
	}
	session.cmd = nil
	session.stdin = nil
	session.stdout = nil
	session.script = ""
}

func (session *tdxSession) request(ctx context.Context, method string, params any, target any) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := session.startLocked(ctx); err != nil {
		return err
	}
	requestID := session.nextID
	session.nextID++
	payload, err := json.Marshal(map[string]any{"id": requestID, "method": method, "params": params})
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	type result struct {
		line []byte
		err  error
	}
	channel := make(chan result, 1)
	go func() {
		if _, writeError := session.stdin.Write(payload); writeError != nil {
			channel <- result{err: writeError}
			return
		}
		line, readError := session.stdout.ReadBytes('\n')
		channel <- result{line: line, err: readError}
	}()
	select {
	case value := <-channel:
		if value.err != nil {
			session.resetLocked()
			return fmt.Errorf("读取 TDX 响应失败: %w", value.err)
		}
		var envelope tdxEnvelope
		if err := json.Unmarshal(bytes.TrimSpace(value.line), &envelope); err != nil {
			session.resetLocked()
			return fmt.Errorf("TDX 响应格式无效: %w", err)
		}
		if !envelope.OK {
			return fmt.Errorf("TDX 请求 %s 失败: %s", method, strings.TrimSpace(envelope.Error))
		}
		if target == nil || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(envelope.Result, target); err != nil {
			return fmt.Errorf("TDX %s 数据格式无效: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		session.resetLocked()
		return ctx.Err()
	}
}

func (session *tdxSession) Close() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.resetLocked()
	return nil
}

type tdxQuote struct {
	Symbol        string              `json:"symbol"`
	Code          string              `json:"code"`
	Current       string              `json:"current"`
	PreviousClose string              `json:"previous_close"`
	Open          string              `json:"open"`
	High          string              `json:"high"`
	Low           string              `json:"low"`
	QuoteTime     string              `json:"quote_time"`
	Delta         float64             `json:"delta"`
	Percent       float64             `json:"percent"`
	Amount        float64             `json:"amount"`
	Volume        float64             `json:"volume"`
	Bids          []domain.DepthLevel `json:"bids"`
	Asks          []domain.DepthLevel `json:"asks"`
}

type cachedTDXMetadata struct {
	quote     domain.Quote
	fetchedAt time.Time
}

type tdxFailure struct {
	error      string
	retryAfter time.Time
}

const (
	tdxOperationQuote  = "行情"
	tdxOperationDaily  = "日K"
	tdxOperationMinute = "分时"
)

// TDXClient provides TCP-backed quotes, daily bars and intraday minute data,
// with the existing HTTP clients as independent transparent fallbacks.
type TDXClient struct {
	session        *tdxSession
	fallbackQuote  QuoteClient
	fallbackDaily  DailyHistoryClient
	fallbackMinute MinuteClient
	metadataTTL    time.Duration
	metadataMu     sync.Mutex
	metadata       map[string]cachedTDXMetadata
	statusMu       sync.Mutex
	failures       map[string]tdxFailure
}

func NewTDXClient(fallbackQuote QuoteClient, fallbackDaily DailyHistoryClient, options TDXOptions) *TDXClient {
	return NewTDXClientWithMinute(fallbackQuote, fallbackDaily, nil, options)
}

func NewTDXClientWithMinute(fallbackQuote QuoteClient, fallbackDaily DailyHistoryClient, fallbackMinute MinuteClient, options TDXOptions) *TDXClient {
	ttl := options.MetadataTTL
	if ttl <= 0 {
		ttl = tdxMetadataTTL
	}
	return &TDXClient{
		session:        newTDXSession(options),
		fallbackQuote:  fallbackQuote,
		fallbackDaily:  fallbackDaily,
		fallbackMinute: fallbackMinute,
		metadataTTL:    ttl,
		metadata:       make(map[string]cachedTDXMetadata),
		failures:       make(map[string]tdxFailure),
	}
}

func (client *TDXClient) recordTDXError(operation string, err error) {
	client.statusMu.Lock()
	defer client.statusMu.Unlock()
	client.failures[operation] = tdxFailure{error: err.Error(), retryAfter: time.Now().Add(tdxRetryBackoff)}
}

func (client *TDXClient) clearTDXError(operation string) {
	client.statusMu.Lock()
	defer client.statusMu.Unlock()
	delete(client.failures, operation)
}

func (client *TDXClient) tdxAllowed(operation string) bool {
	client.statusMu.Lock()
	defer client.statusMu.Unlock()
	failure, ok := client.failures[operation]
	return !ok || time.Now().After(failure.retryAfter)
}

func (client *TDXClient) Status() string {
	client.statusMu.Lock()
	defer client.statusMu.Unlock()
	active := make([]string, 0, len(client.failures))
	for _, operation := range []string{tdxOperationQuote, tdxOperationDaily, tdxOperationMinute} {
		failure, ok := client.failures[operation]
		if ok && time.Now().Before(failure.retryAfter) {
			active = append(active, operation+": "+failure.error)
		}
	}
	if len(active) > 0 {
		return "通达信 TCP 部分不可用，HTTP 回退: " + strings.Join(active, "；")
	}
	return "通达信 TCP"
}

func (client *TDXClient) metadataFor(ctx context.Context, symbols []string) map[string]domain.Quote {
	missing := make([]string, 0, len(symbols))
	now := time.Now()
	client.metadataMu.Lock()
	for _, symbol := range symbols {
		entry, ok := client.metadata[symbol]
		if !ok || now.Sub(entry.fetchedAt) >= client.metadataTTL {
			missing = append(missing, symbol)
		}
	}
	client.metadataMu.Unlock()
	if len(missing) > 0 && client.fallbackQuote != nil {
		if quotes, err := client.fallbackQuote.Fetch(ctx, missing); err == nil {
			client.metadataMu.Lock()
			for _, quote := range quotes {
				client.metadata[quote.Symbol] = cachedTDXMetadata{quote: quote, fetchedAt: now}
			}
			client.metadataMu.Unlock()
		}
	}
	result := make(map[string]domain.Quote, len(symbols))
	client.metadataMu.Lock()
	defer client.metadataMu.Unlock()
	for _, symbol := range symbols {
		if entry, ok := client.metadata[symbol]; ok {
			result[symbol] = entry.quote
		}
	}
	return result
}

func mergeTDXQuote(raw tdxQuote, metadata domain.Quote) domain.Quote {
	quote := metadata
	quote.Symbol = raw.Symbol
	quote.Source = "通达信TCP"
	quote.Code = raw.Code
	quote.Current = raw.Current
	quote.PreviousClose = raw.PreviousClose
	quote.Open = raw.Open
	quote.High = raw.High
	quote.Low = raw.Low
	quote.QuoteTime = raw.QuoteTime
	quote.Delta = raw.Delta
	quote.Percent = raw.Percent
	quote.Amount = raw.Amount
	quote.Volume = raw.Volume
	quote.Bids = raw.Bids
	quote.Asks = raw.Asks
	if quote.TaskName == "" {
		quote.TaskName = quote.Name
	}
	return quote
}

func (client *TDXClient) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	tdxSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if ValidPrefixedSymbol(symbol) {
			tdxSymbols = append(tdxSymbols, symbol)
		}
	}
	var rows []tdxQuote
	var tdxError error
	tdxAttempted := false
	if len(tdxSymbols) > 0 && client.tdxAllowed(tdxOperationQuote) {
		tdxAttempted = true
		tdxError = client.session.request(ctx, "quote", map[string]any{"symbols": tdxSymbols}, &rows)
		if tdxError != nil {
			client.recordTDXError(tdxOperationQuote, tdxError)
		}
	}
	if tdxError == nil && len(rows) > 0 {
		client.clearTDXError(tdxOperationQuote)
		metadata := client.metadataFor(ctx, symbols)
		bySymbol := make(map[string]tdxQuote, len(rows))
		for _, row := range rows {
			bySymbol[row.Symbol] = row
		}
		result := make([]domain.Quote, 0, len(symbols))
		for _, symbol := range symbols {
			if row, ok := bySymbol[symbol]; ok {
				result = append(result, mergeTDXQuote(row, metadata[symbol]))
			} else if quote, ok := metadata[symbol]; ok {
				result = append(result, quote)
			}
		}
		return result, nil
	}
	if client.fallbackQuote == nil {
		if tdxError == nil {
			tdxError = fmt.Errorf("TDX 未返回行情")
		}
		return nil, tdxError
	}
	if tdxAttempted && tdxError == nil && len(rows) == 0 && len(tdxSymbols) > 0 {
		tdxError = fmt.Errorf("TDX 未返回行情")
		client.recordTDXError(tdxOperationQuote, tdxError)
	}
	quotes, fallbackError := client.fallbackQuote.Fetch(ctx, symbols)
	if fallbackError != nil && tdxError != nil {
		return nil, fmt.Errorf("通达信 TCP: %v；HTTP 回退: %w", tdxError, fallbackError)
	}
	return quotes, fallbackError
}

func validTDXBar(bar domain.DailyBar) bool {
	return bar.Date != "" && bar.Open > 0 && bar.Close > 0 && bar.High > 0 && bar.Low > 0 &&
		bar.High >= bar.Low && bar.Volume >= 0 && !math.IsNaN(bar.Close)
}

func (client *TDXClient) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	var bars []domain.DailyBar
	var tdxError error
	tdxAttempted := false
	if client.tdxAllowed(tdxOperationDaily) {
		tdxAttempted = true
		tdxError = client.session.request(ctx, "bars", map[string]any{"symbol": symbol, "count": dailyHistoryLimit}, &bars)
		if tdxError != nil {
			client.recordTDXError(tdxOperationDaily, tdxError)
		}
	}
	valid := bars[:0]
	for _, bar := range bars {
		if validTDXBar(bar) {
			valid = append(valid, bar)
		}
	}
	if tdxError == nil && len(valid) >= 60 {
		client.clearTDXError(tdxOperationDaily)
		return valid, nil
	}
	if client.fallbackDaily == nil {
		if tdxError == nil {
			tdxError = fmt.Errorf("TDX 仅返回 %d 根有效日 K", len(valid))
		}
		return nil, tdxError
	}
	if tdxAttempted && tdxError == nil && len(valid) < 60 {
		tdxError = fmt.Errorf("TDX 仅返回 %d 根有效日 K", len(valid))
		client.recordTDXError(tdxOperationDaily, tdxError)
	}
	bars, fallbackError := client.fallbackDaily.FetchDailyBars(ctx, symbol)
	if fallbackError != nil && tdxError != nil {
		return nil, fmt.Errorf("通达信 TCP: %v；HTTP 回退: %w", tdxError, fallbackError)
	}
	return bars, fallbackError
}

func validTDXMinutePoint(point domain.MinutePoint) bool {
	clock := strings.ReplaceAll(point.Time, ":", "")
	return point.TradeDate != "" && validMinuteTime(clock) && point.Price > 0 && point.Average > 0 &&
		point.Volume >= 0 && !math.IsNaN(point.Price) && !math.IsInf(point.Price, 0)
}

func (client *TDXClient) FetchMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	if !ValidPrefixedSymbol(symbol) {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	var points []domain.MinutePoint
	var tdxError error
	tdxAttempted := false
	if client.tdxAllowed(tdxOperationMinute) {
		tdxAttempted = true
		tdxError = client.session.request(ctx, "minutes", map[string]any{"symbol": symbol}, &points)
		if tdxError != nil {
			client.recordTDXError(tdxOperationMinute, tdxError)
		}
	}
	valid := points[:0]
	for _, point := range points {
		if validTDXMinutePoint(point) {
			valid = append(valid, point)
		}
	}
	if tdxError == nil && len(valid) > 0 {
		client.clearTDXError(tdxOperationMinute)
		return valid, nil
	}
	if client.fallbackMinute == nil {
		if tdxError == nil {
			tdxError = fmt.Errorf("TDX 未返回有效分时行情")
		}
		return nil, tdxError
	}
	if tdxAttempted && tdxError == nil && len(valid) == 0 {
		tdxError = fmt.Errorf("TDX 未返回有效分时行情")
		client.recordTDXError(tdxOperationMinute, tdxError)
	}
	points, fallbackError := client.fallbackMinute.FetchMinutePoints(ctx, symbol)
	if fallbackError != nil && tdxError != nil {
		return nil, fmt.Errorf("通达信 TCP: %v；HTTP 回退: %w", tdxError, fallbackError)
	}
	return points, fallbackError
}

func (client *TDXClient) Close() error {
	return client.session.Close()
}
