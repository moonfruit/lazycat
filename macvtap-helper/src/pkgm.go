package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Pkgm struct {
	Base string
	HTTP *http.Client
}

func NewPkgm() *Pkgm {
	return &Pkgm{
		Base: "http://127.0.0.1:7733/api/app",
		HTTP: &http.Client{Timeout: 180 * time.Second},
	}
}

func (p *Pkgm) Status(id string) (int, error) {
	u := fmt.Sprintf("%s/instance/status?id=%s", p.Base, url.QueryEscape(id))
	resp, err := p.HTTP.Get(u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("status http %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, fmt.Errorf("decode status: %w", err)
	}
	return out.Status, nil
}

func (p *Pkgm) post(action, id, uid string) (int, []byte, error) {
	u := fmt.Sprintf("%s/instance/%s?id=%s&uid=%s",
		p.Base, action, url.QueryEscape(id), url.QueryEscape(uid))
	resp, err := p.HTTP.Post(u, "", nil)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

// Resume 启动/恢复实例。忽略 HTTP 400（已知残留实例 NAT reconcile 报错），其它非 2xx 报错。
func (p *Pkgm) Resume(id, uid string) error {
	code, body, err := p.post("resume", id, uid)
	if err != nil {
		return err
	}
	if code == http.StatusBadRequest {
		return nil // 忽略 400
	}
	if code/100 != 2 {
		return fmt.Errorf("resume http %d: %s", code, body)
	}
	return nil
}

// Pause 停止实例。pause 失败不致命（实例本就可能未运行），仅返回错误供调用方记录。
func (p *Pkgm) Pause(id, uid string) error {
	code, body, err := p.post("pause", id, uid)
	if err != nil {
		return err
	}
	if code/100 != 2 && code != http.StatusBadRequest {
		return fmt.Errorf("pause http %d: %s", code, body)
	}
	return nil
}
