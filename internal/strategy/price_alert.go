package strategy

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

var priceAlertLocation = time.FixedZone("Asia/Shanghai", 8*3600)
var priceAlertSymbol = regexp.MustCompile(`^(sh6[0-9]{5}|sz(00|30)[0-9]{4}|bj([48][0-9]{5}|92[0-9]{4}))$`)
var priceAlertID = regexp.MustCompile(`^price-[a-zA-Z0-9-]{8,80}$`)

func ValidatePriceAlertRule(rule domain.PriceAlertRule) error {
	if !priceAlertSymbol.MatchString(rule.Symbol) || rule.Symbol == "bj899050" || rule.Direction != "above" && rule.Direction != "below" || rule.Price <= 0 || rule.Price > 1e8 || math.IsNaN(rule.Price) || math.IsInf(rule.Price, 0) || len([]rune(rule.Name)) > 80 {
		return fmt.Errorf("提醒股票、方向、价位或名称无效")
	}
	if _, err := time.Parse(time.DateOnly, rule.ExpiresOn); err != nil {
		return fmt.Errorf("提醒有效期无效")
	}
	if rule.Source != nil {
		if rule.Source.ID == "" || len(rule.Source.Fingerprint) != 64 {
			return fmt.Errorf("来源快照标识无效")
		}
		if _, err := time.Parse(time.DateOnly, rule.Source.Date); err != nil {
			return fmt.Errorf("来源日期无效")
		}
	}
	return nil
}

func ValidatePriceAlert(alert domain.PriceAlert) error {
	if err := ValidatePriceAlertRule(alert.Rule); err != nil {
		return err
	}
	if !priceAlertID.MatchString(alert.ID) || alert.Revision == 0 || alert.CreatedAt.IsZero() || alert.UpdatedAt.Before(alert.CreatedAt) || alert.Enabled && alert.EnabledAt.IsZero() {
		return fmt.Errorf("提醒记录标识或时间无效")
	}
	if alert.Event != nil {
		e := alert.Event
		if alert.Enabled || e.ID != "price-alert:"+alert.ID || e.ObservedAt.IsZero() || e.QuoteAt.IsZero() || e.QuoteAt.After(e.ObservedAt) || e.Price <= 0 || math.IsNaN(e.Price) || math.IsInf(e.Price, 0) {
			return fmt.Errorf("提醒触发事件无效")
		}
	}
	if !alert.DeletedAt.IsZero() && alert.Enabled {
		return fmt.Errorf("已删除提醒不能启用")
	}
	return nil
}

func PriceAlertSession(now time.Time) bool {
	local := now.In(priceAlertLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return false
	}
	minute := local.Hour()*60 + local.Minute()
	return minute >= 570 && minute <= 690 || minute >= 780 && minute <= 900
}

func PriceAlertPhase(alert domain.PriceAlert, now time.Time) string {
	if !alert.DeletedAt.IsZero() {
		return "deleted"
	}
	if alert.Event != nil {
		return "triggered"
	}
	if alert.Rule.ExpiresOn < now.In(priceAlertLocation).Format(time.DateOnly) {
		return "expired"
	}
	if !alert.Enabled {
		return "paused"
	}
	return "active"
}

// The rule is immutable. Evaluation can only update observations or append its
// one terminal trigger event; a later enable/pause wins over an older fetch.
func AdvancePriceAlert(alert domain.PriceAlert, now time.Time, quote domain.Quote, fetchError string) domain.PriceAlert {
	if now.Before(alert.UpdatedAt) || !alert.DeletedAt.IsZero() || alert.Event != nil {
		return alert
	}
	if PriceAlertPhase(alert, now) == "expired" {
		if !alert.Enabled && alert.CheckState == "expired" {
			return alert
		}
		alert.Enabled = false
		alert.UpdatedAt = now
		alert.CheckState = "expired"
		return alert
	}
	if !alert.Enabled {
		return alert
	}
	alert.LastCheckedAt = now
	alert.UpdatedAt = now
	if !PriceAlertSession(now) {
		alert.CheckState = "closed"
		alert.CheckDetail = "等待交易时段"
		return alert
	}
	if fetchError != "" {
		alert.CheckState = "error"
		alert.CheckDetail = fetchError
		return alert
	}
	var at time.Time
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(quote.QuoteTime), priceAlertLocation); err == nil {
			at = parsed
			break
		}
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
	if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) || quote.Symbol != alert.Rule.Symbol || at.IsZero() || at.After(now) || now.Sub(at) > 90*time.Second || !PriceAlertSession(at) || at.In(priceAlertLocation).Format(time.DateOnly) != now.In(priceAlertLocation).Format(time.DateOnly) {
		alert.CheckState = "stale"
		alert.CheckDetail = "等待当日新鲜报价"
		return alert
	}
	if at.Before(alert.EnabledAt) || !alert.QuoteAt.IsZero() && !at.After(alert.QuoteAt) {
		alert.CheckState = "waiting"
		alert.CheckDetail = "等待启用后的新报价"
		return alert
	}
	alert.QuoteAt = at
	alert.QuotePrice = &price
	alert.CheckState = "watching"
	alert.CheckDetail = "尚未到达价位"
	if alert.Rule.Direction == "above" && price >= alert.Rule.Price || alert.Rule.Direction == "below" && price <= alert.Rule.Price {
		alert.Event = &domain.PriceAlertEvent{ID: "price-alert:" + alert.ID, ObservedAt: now, QuoteAt: at, Price: price}
		alert.Enabled = false
		alert.CheckState = "triggered"
		alert.CheckDetail = "价位已触发"
	}
	return alert
}
