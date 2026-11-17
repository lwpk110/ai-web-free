package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	appdb "ai-web-free/internal/db"

	"gorm.io/gorm"
)

var (
	grokCredentialPlaceholderPattern = regexp.MustCompile(`\{(email|password|totp|legacy_token|sso|sso_rw|proxy|cookie)\}`)
	grokPossibleSecretPattern        = regexp.MustCompile(`[A-Za-z0-9_/+=:.@-]{12,}`)
)

// SetGrokCredentialPatternDB attaches the persistent credential-format memory database.
func (p *GrokProvider) SetGrokCredentialPatternDB(database *gorm.DB) {
	p.grokCredentialPatternMu.Lock()
	defer p.grokCredentialPatternMu.Unlock()
	p.grokCredentialPatternDB = database
}

func (p *GrokProvider) learnedGrokCredentialPatterns() []appdb.CredentialFormatPatternRecord {
	p.grokCredentialPatternMu.RLock()
	database := p.grokCredentialPatternDB
	p.grokCredentialPatternMu.RUnlock()
	if database == nil {
		return nil
	}

	var patterns []appdb.CredentialFormatPatternRecord
	if err := database.
		Where("provider = ? AND enabled = ?", "grok", true).
		Order("updated_at desc").
		Limit(1000).
		Find(&patterns).Error; err != nil {
		return nil
	}
	return patterns
}

func (p *GrokProvider) recordGrokCredentialPatternUse(id string, success bool) {
	p.grokCredentialPatternMu.RLock()
	database := p.grokCredentialPatternDB
	p.grokCredentialPatternMu.RUnlock()
	if database == nil || strings.TrimSpace(id) == "" {
		return
	}

	var record appdb.CredentialFormatPatternRecord
	if err := database.First(&record, "id = ?", id).Error; err != nil {
		return
	}
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"use_count":    record.UseCount + 1,
		"updated_at":   now,
		"last_used_at": now,
	}
	if success {
		updates["success_count"] = record.SuccessCount + 1
	} else {
		failures := record.FailureCount + 1
		updates["failure_count"] = failures
		if failures >= 3 {
			updates["enabled"] = false
		}
	}
	_ = database.Model(&appdb.CredentialFormatPatternRecord{}).Where("id = ?", id).Updates(updates).Error
}

func (p *GrokProvider) learnGrokCredentialPattern(original, normalized, kind string) bool {
	record, err := buildGrokCredentialPatternRecord(original, normalized, kind)
	if err != nil {
		return false
	}

	p.grokCredentialPatternMu.RLock()
	database := p.grokCredentialPatternDB
	p.grokCredentialPatternMu.RUnlock()
	if database == nil {
		return false
	}

	var existing appdb.CredentialFormatPatternRecord
	err = database.First(&existing, "provider = ? AND input_template = ?", "grok", record.InputTemplate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.Create(&record).Error == nil
	}
	if err != nil {
		return false
	}
	updates := map[string]interface{}{"updated_at": time.Now().UTC()}
	if !existing.Enabled && existing.OutputTemplate == record.OutputTemplate {
		updates["enabled"] = true
		updates["failure_count"] = 0
	}
	return database.Model(&appdb.CredentialFormatPatternRecord{}).Where("id = ?", existing.ID).Updates(updates).Error == nil
}

func buildGrokCredentialPatternRecord(original, normalized, credentialKind string) (appdb.CredentialFormatPatternRecord, error) {
	values := grokCredentialTemplateValues(normalized, credentialKind)
	if len(values) == 0 {
		return appdb.CredentialFormatPatternRecord{}, errors.New("no replaceable credential values")
	}
	inputTemplate := original
	outputTemplate := normalized
	for _, item := range orderedGrokTemplateValues(values) {
		inputTemplate = replaceGrokTemplateValue(inputTemplate, item.value, item.placeholder, item.normalizeTOTP)
		outputTemplate = strings.Replace(outputTemplate, item.value, item.placeholder, 1)
	}
	if !strings.Contains(inputTemplate, "{") || !strings.Contains(outputTemplate, "{") {
		return appdb.CredentialFormatPatternRecord{}, errors.New("template placeholders were not generated")
	}
	for _, value := range values {
		if value != "" && strings.Contains(inputTemplate, value) {
			return appdb.CredentialFormatPatternRecord{}, errors.New("input template still contains a credential value")
		}
	}
	if grokTemplateMayContainSecret(inputTemplate) {
		return appdb.CredentialFormatPatternRecord{}, errors.New("input template may contain an unrecognized secret")
	}

	sum := sha256.Sum256([]byte("grok\n" + inputTemplate))
	return appdb.CredentialFormatPatternRecord{
		ID:             hex.EncodeToString(sum[:]),
		Provider:       "grok",
		CredentialType: credentialKind,
		InputTemplate:  inputTemplate,
		OutputTemplate: outputTemplate,
		Enabled:        true,
	}, nil
}

type grokTemplateValue struct {
	value         string
	placeholder   string
	normalizeTOTP bool
}

func grokCredentialTemplateValues(normalized, credentialKind string) map[string]string {
	values := make(map[string]string)
	if credentialKind == "kami" {
		card, err := ParseGrokCardLine(normalized, 1)
		if err != nil {
			return values
		}
		if card.Email != "" {
			values["email"] = card.Email
		}
		if card.Password != "" {
			values["password"] = card.Password
		}
		if card.TOTPSecret != "" {
			values["totp"] = card.TOTPSecret
		}
		if card.LegacyToken != "" {
			values["legacy_token"] = card.LegacyToken
		}
		if card.Proxy != "" {
			values["proxy"] = card.Proxy
		}
		addGrokCookieTemplateValues(values, card.OldCookie)
		return values
	}

	cookie := strings.TrimSpace(normalized)
	if strings.Contains(cookie, "----") {
		parts := strings.SplitN(cookie, "----", 2)
		if email := strings.TrimSpace(parts[0]); email != "" {
			values["email"] = email
		}
		cookie = strings.TrimSpace(parts[1])
	}
	addGrokCookieTemplateValues(values, cookie)
	return values
}

func addGrokCookieTemplateValues(values map[string]string, cookie string) {
	if cookie == "" {
		return
	}
	for _, match := range grokCookieValuePattern.FindAllStringSubmatch(cookie, -1) {
		key := strings.ToLower(strings.TrimSpace(match[1]))
		value := strings.TrimSpace(match[2])
		if value == "" {
			continue
		}
		if key == "sso-rw" {
			values["sso_rw"] = value
		} else if key == "sso" {
			values["sso"] = value
		}
	}
}

func orderedGrokTemplateValues(values map[string]string) []grokTemplateValue {
	out := make([]grokTemplateValue, 0, len(values))
	for placeholder, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, grokTemplateValue{
			value:         value,
			placeholder:   "{" + placeholder + "}",
			normalizeTOTP: placeholder == "totp",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].value) != len(out[j].value) {
			return len(out[i].value) > len(out[j].value)
		}
		return out[i].placeholder < out[j].placeholder
	})
	return out
}

func grokTemplateMayContainSecret(template string) bool {
	withoutPlaceholders := grokCredentialPlaceholderPattern.ReplaceAllString(template, " ")
	for _, field := range strings.Fields(withoutPlaceholders) {
		if grokPossibleSecretPattern.MatchString(field) {
			return true
		}
	}
	return false
}

func replaceGrokTemplateValue(source, value, placeholder string, normalizeTOTP bool) string {
	if value == "" {
		return source
	}
	if index := strings.Index(source, value); index >= 0 {
		return source[:index] + placeholder + source[index+len(value):]
	}
	if !normalizeTOTP {
		return source
	}

	normalize := func(text string) string {
		return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(text))
	}
	normalizedValue := normalize(value)
	runes := []rune(source)
	for start := 0; start < len(runes); start++ {
		for end := start + 1; end <= len(runes); end++ {
			if normalize(string(runes[start:end])) == normalizedValue {
				prefix := string(runes[:start])
				suffix := string(runes[end:])
				return prefix + placeholder + suffix
			}
		}
	}
	return source
}

func applyGrokCredentialPattern(line string, pattern appdb.CredentialFormatPatternRecord) (string, string, bool, bool) {
	expression, names, err := compileGrokCredentialTemplate(pattern.InputTemplate)
	if err != nil {
		return "", "", false, false
	}
	matches := expression.FindStringSubmatch(line)
	if matches == nil {
		return "", "", false, false
	}
	captured := make(map[string]string, len(names))
	for index, name := range names {
		if index+1 < len(matches) {
			captured[name] = matches[index+1]
		}
	}
	rendered := renderGrokCredentialTemplate(pattern.OutputTemplate, captured)
	kind, _, detectErr := DetectGrokCredentialLine(rendered)
	if detectErr != nil {
		return "", "", false, true
	}
	if _, validateErr := validateGrokCredentialLLMResult(line, grokCredentialLLMResult{
		Index: 0,
		Kind:  kind,
		Value: rendered,
	}); validateErr != nil {
		return "", "", false, true
	}
	return kind, rendered, true, true
}

func compileGrokCredentialTemplate(template string) (*regexp.Regexp, []string, error) {
	names := make([]string, 0, 6)
	var expression strings.Builder
	expression.WriteString(`(?s)^`)
	position := 0
	for _, match := range grokCredentialPlaceholderPattern.FindAllStringSubmatchIndex(template, -1) {
		start, end := match[0], match[1]
		name := template[match[2]:match[3]]
		expression.WriteString(regexp.QuoteMeta(template[position:start]))
		expression.WriteString("(.+?)")
		names = append(names, name)
		position = end
	}
	expression.WriteString(regexp.QuoteMeta(template[position:]))
	expression.WriteString("$")
	compiled, err := regexp.Compile(expression.String())
	if err != nil {
		return nil, nil, err
	}
	if len(names) == 0 {
		return nil, nil, errors.New("credential template has no placeholder")
	}
	return compiled, names, nil
}

func renderGrokCredentialTemplate(template string, values map[string]string) string {
	replacements := make([]string, 0, len(values)*2)
	for name, value := range values {
		replacements = append(replacements, "{"+name+"}", value)
	}
	if len(replacements) == 0 {
		return template
	}
	return strings.NewReplacer(replacements...).Replace(template)
}

func (p *GrokProvider) matchLearnedGrokCredential(line string, patterns []appdb.CredentialFormatPatternRecord) (string, string, bool) {
	for _, pattern := range patterns {
		kind, value, ok, attempted := applyGrokCredentialPattern(line, pattern)
		if attempted {
			p.recordGrokCredentialPatternUse(pattern.ID, ok)
		}
		if ok {
			return kind, value, true
		}
	}
	return "", "", false
}
