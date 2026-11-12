package minercontrol

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	loginKeyByte = byte(0x21)
	requestLimit = 1_000_000
)

var secretValueRE = regexp.MustCompile(`(?i)((password|pass|token|authorization)=)([^&\s]+)`)

// Controller is the dashboard seam for privileged miner actions.
type Controller interface {
	ApplyPoolConfig(context.Context, Target, PoolConfig) (Result, error)
	Restart(context.Context, Target, RestartRequest) (Result, error)
}

type Target struct {
	IP  string
	MAC string
}

type PoolConfig struct {
	AdminPassword   string
	PoolURL         string
	WorkerUser      string
	PoolPassword    string
	ReplaceExisting bool
}

type RestartRequest struct {
	AdminPassword string
}

type Evidence struct {
	Label string
	Value string
}

type Result struct {
	Tone    string
	Title   string
	Message string
	Items   []Evidence
}

type ASICConfig struct {
	HTTPClient        *http.Client
	AllowedCIDRs      string
	AllowDockerBridge bool
	Sleep             func(time.Duration)
}

type ASICController struct {
	http              *http.Client
	allowedCIDRs      string
	allowDockerBridge bool
	sleep             func(time.Duration)
}

type httpError struct {
	status int
	msg    string
}

func (e httpError) Error() string { return e.msg }

func NewASICController(config ASICConfig) *ASICController {
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	sleep := config.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	return &ASICController{
		http:              client,
		allowedCIDRs:      config.AllowedCIDRs,
		allowDockerBridge: config.AllowDockerBridge,
		sleep:             sleep,
	}
}

func (c *ASICController) ApplyPoolConfig(ctx context.Context, target Target, config PoolConfig) (Result, error) {
	if err := c.validateTarget(target); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(config.AdminPassword) == "" {
		return Result{}, fmt.Errorf("admin password is required")
	}
	config.PoolURL = strings.TrimSpace(config.PoolURL)
	config.WorkerUser = strings.TrimSpace(config.WorkerUser)
	if config.PoolURL == "" || config.WorkerUser == "" {
		return Result{}, fmt.Errorf("pool URL and worker/wallet are required")
	}
	if config.PoolPassword == "" {
		config.PoolPassword = "1234"
	}
	desired := map[string]string{"url": config.PoolURL, "user": config.WorkerUser, "pass": config.PoolPassword}
	before, err := c.getPools(ctx, target.IP)
	if err != nil {
		return Result{}, err
	}
	token, err := c.login(ctx, target.IP, config.AdminPassword)
	if err != nil {
		return Result{}, err
	}
	if !poolListContains(before, desired) {
		payload := map[string]any{"url": config.PoolURL, "user": config.WorkerUser, "pass": config.PoolPassword}
		if _, err := c.putAuth(ctx, target.IP, "/mcb/newpool", payload, token); err != nil {
			payload["pool-priority"] = 0
			if _, retryErr := c.putAuth(ctx, target.IP, "/mcb/newpool", payload, token); retryErr != nil {
				return Result{}, retryErr
			}
		}
		c.sleep(600 * time.Millisecond)
	}
	afterAdd, err := c.getPools(ctx, target.IP)
	if err != nil {
		return Result{}, err
	}
	if !poolListContains(afterAdd, desired) {
		return Result{}, fmt.Errorf("%s did not accept the new pool entry", target.IP)
	}
	deleted := 0
	deleteFailures := 0
	if config.ReplaceExisting {
		pools := append([]map[string]any{}, afterAdd...)
		sort.SliceStable(pools, func(i, j int) bool {
			return number(mapString(pools[i], "pool-priority")) > number(mapString(pools[j], "pool-priority"))
		})
		keptDesired := false
		for _, pool := range pools {
			if poolMatchesDesired(pool, desired) && !keptDesired {
				keptDesired = true
				continue
			}
			if err := c.deletePool(ctx, target.IP, token, pool); err != nil {
				deleteFailures++
				continue
			}
			deleted++
			c.sleep(200 * time.Millisecond)
		}
	}
	if _, err := c.putAuth(ctx, target.IP, "/mcb/pools", []map[string]any{{
		"url": config.PoolURL, "user": config.WorkerUser, "pass": config.PoolPassword, "pool-priority": 0,
	}}, token); err != nil {
		deleteFailures++
	}
	c.sleep(600 * time.Millisecond)
	finalPools, err := c.getPools(ctx, target.IP)
	if err != nil {
		return Result{}, err
	}
	active := false
	for _, pool := range finalPools {
		if poolMatchesDesired(pool, desired) {
			active = truthyValue(pool["active"])
			break
		}
	}
	tone := "ok"
	title := "Pool configuration applied"
	message := "The miner pool list now includes the requested pool."
	if deleteFailures > 0 || !active {
		tone = "warn"
		title = "Pool configuration partially applied"
		message = "The requested pool exists, but the miner did not report it active yet or some older pool entries could not be removed."
	}
	return Result{
		Tone:    tone,
		Title:   title,
		Message: message,
		Items: []Evidence{
			{"Miner", first(strings.ToUpper(target.MAC), target.IP)},
			{"IP", target.IP},
			{"Pool URL", config.PoolURL},
			{"Worker", shortAddress(config.WorkerUser)},
			{"Pool Active", yesNo(active)},
			{"Removed Old Pools", strconv.Itoa(deleted)},
			{"Remove Failures", strconv.Itoa(deleteFailures)},
		},
	}, nil
}

func (c *ASICController) Restart(ctx context.Context, target Target, request RestartRequest) (Result, error) {
	if err := c.validateTarget(target); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(request.AdminPassword) == "" {
		return Result{}, fmt.Errorf("admin password is required")
	}
	token, err := c.login(ctx, target.IP, request.AdminPassword)
	if err != nil {
		return Result{}, err
	}
	if _, err := c.putAuth(ctx, target.IP, "/mcb/restart", map[string]any{}, token); err != nil {
		return Result{}, err
	}
	return Result{
		Tone:    "ok",
		Title:   "Restart requested",
		Message: "The miner accepted the restart command.",
		Items:   []Evidence{{"Miner", first(strings.ToUpper(target.MAC), target.IP)}, {"IP", target.IP}},
	}, nil
}

func (c *ASICController) validateTarget(target Target) error {
	return validateMinerIP(target.IP, c.allowedCIDRs, c.allowDockerBridge)
}

func (c *ASICController) getPools(ctx context.Context, ip string) ([]map[string]any, error) {
	body, _, err := c.request(ctx, ip, "/mcb/pools", http.MethodGet, nil, "", false)
	if err != nil {
		return nil, err
	}
	return asMapList(body), nil
}

func (c *ASICController) login(ctx context.Context, ip string, password string) (string, error) {
	cipherText, err := encryptPassword(password)
	if err != nil {
		return "", err
	}
	query := url.Values{"username": {"admin"}, "password": {cipherText}, "cipher": {"true"}}.Encode()
	body, _, err := c.request(ctx, ip, "/user/login?"+query, http.MethodGet, nil, "", false)
	if err != nil {
		return "", err
	}
	response, ok := body.(map[string]any)
	if !ok {
		return "", fmt.Errorf("%s login returned a non-object response", ip)
	}
	token := first(mapString(response, "JWT Token"), mapString(response, "token"), mapString(response, "jwt"), mapString(response, "JWT"))
	if token == "" {
		return "", fmt.Errorf("%s login did not return an access token", ip)
	}
	return token, nil
}

func (c *ASICController) putAuth(ctx context.Context, ip string, path string, payload any, token string) (any, error) {
	body, status, err := c.request(ctx, ip, path, http.MethodPut, payload, token, false)
	if err == nil {
		return body, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return nil, err
	}
	body, _, retryErr := c.request(ctx, ip, path, http.MethodPut, payload, token, true)
	return body, retryErr
}

func (c *ASICController) request(ctx context.Context, ip string, path string, method string, payload any, token string, rawAuthorization bool) (any, int, error) {
	if err := validateMinerIP(ip, c.allowedCIDRs, c.allowDockerBridge); err != nil {
		return nil, 0, err
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+ip+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("accept", "application/json")
	if payload != nil {
		req.Header.Set("content-type", "application/json;charset=UTF-8")
	}
	if token != "" {
		if rawAuthorization {
			req.Header.Set("authorization", token)
		} else {
			req.Header.Set("authorization", "Bearer "+token)
		}
		req.Header.Set("x-access-token", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("miner request failed for %s%s: %w", ip, RedactSecrets(path), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, requestLimit))
	var parsed any
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			parsed = string(raw)
		}
	}
	if resp.StatusCode >= 400 {
		return parsed, resp.StatusCode, httpError{status: resp.StatusCode, msg: fmt.Sprintf("miner HTTP %d for %s%s", resp.StatusCode, ip, RedactSecrets(path))}
	}
	return parsed, resp.StatusCode, nil
}

func (c *ASICController) deletePool(ctx context.Context, ip string, token string, pool map[string]any) error {
	attempts := []map[string]any{cloneMap(pool)}
	if priority := mapString(pool, "pool-priority"); priority != "" {
		if parsed, err := strconv.Atoi(priority); err == nil {
			attempts = append(attempts, map[string]any{"pool-priority": parsed})
		} else {
			attempts = append(attempts, map[string]any{"pool-priority": priority})
		}
	}
	if poolURL := mapString(pool, "url"); poolURL != "" {
		attempts = append(attempts, map[string]any{"url": poolURL, "user": mapString(pool, "user"), "pass": mapString(pool, "pass")})
	}
	var lastErr error
	for _, payload := range attempts {
		if _, err := c.putAuth(ctx, ip, "/mcb/delpool", payload, token); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func encryptPassword(password string) (string, error) {
	key := bytes.Repeat([]byte{loginKeyByte}, aes.BlockSize)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	plain := []byte(password)
	padLen := (aes.BlockSize - (len(plain) % aes.BlockSize)) % aes.BlockSize
	if padLen > 0 {
		plain = append(plain, bytes.Repeat([]byte{0}, padLen)...)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(out, plain)
	return fmt.Sprintf("%x", out), nil
}

func validateMinerIP(ipText string, allowedCIDRs string, allowDockerBridge bool) error {
	ip := net.ParseIP(strings.TrimSpace(ipText))
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("miner IP must be an IPv4 address")
	}
	if !ip.IsPrivate() {
		return fmt.Errorf("refusing non-private miner address: %s", ipText)
	}
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("refusing non-LAN miner address: %s", ipText)
	}
	if strings.TrimSpace(allowedCIDRs) != "" && !ipInConfiguredCIDRs(ip, allowedCIDRs) {
		return fmt.Errorf("refusing miner address outside BDAG_ASIC_LAN_CIDRS: %s", ipText)
	}
	if !allowDockerBridge && ipInCIDR(ip, "172.16.0.0/12") {
		return fmt.Errorf("refusing Docker bridge-range miner address: %s", ipText)
	}
	return nil
}

func ipInConfiguredCIDRs(ip net.IP, cidrs string) bool {
	for _, token := range strings.FieldsFunc(cidrs, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if !strings.Contains(token, "/") {
			parsed := net.ParseIP(token)
			if parsed != nil && parsed.Equal(ip) {
				return true
			}
			continue
		}
		if ipInCIDR(ip, token) {
			return true
		}
	}
	return false
}

func ipInCIDR(ip net.IP, cidr string) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return network.Contains(ip)
}

func asMapList(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func poolListContains(pools []map[string]any, desired map[string]string) bool {
	for _, pool := range pools {
		if poolMatchesDesired(pool, desired) {
			return true
		}
	}
	return false
}

func poolMatchesDesired(pool map[string]any, desired map[string]string) bool {
	return mapString(pool, "url") == desired["url"] &&
		mapString(pool, "user") == desired["user"] &&
		mapString(pool, "pass") == desired["pass"]
}

func truthyValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "connected":
			return true
		}
	case float64:
		return v > 0
	case int:
		return v > 0
	case int64:
		return v > 0
	}
	return false
}

func mapString(m map[string]any, key string) string {
	return stringify(m[key])
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if float64(int64(x)) == x {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func number(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func shortAddress(address string) string {
	address = strings.TrimSpace(address)
	if len(address) <= 12 {
		return address
	}
	return address[:6] + "..." + address[len(address)-4:]
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func RedactSecrets(value string) string {
	return secretValueRE.ReplaceAllString(value, "${1}<redacted>")
}
