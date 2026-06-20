package main

import (
	"html/template"
	"log"
	"net/http"
)

var statusTmpl = template.Must(template.New("status").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>HAOS Helper</title>
<style>body{font-family:sans-serif;max-width:640px;margin:40px auto;padding:0 16px}
.k{color:#666}.v{font-weight:600}form{display:inline}button{padding:8px 14px;margin:6px 6px 0 0;cursor:pointer}</style>
</head><body>
<h1>HAOS macvtap Helper</h1>
<p><span class="k">macvtap 模块:</span> <span class="v">{{if .MacvtapLoaded}}已加载{{else}}未加载{{end}}</span></p>
<p><span class="k">lightos 实例状态码:</span> <span class="v">{{.InstanceStatus}}</span></p>
<p><span class="k">最近消息:</span> <span class="v">{{.Message}}</span></p>
<form method="post" action="load-macvtap"><button type="submit">强制加载 macvtap</button></form>
<form method="post" action="restart-lightos"><button type="submit">重启 lightos</button></form>
</body></html>`))

func webHandler(call func(Request) (Response, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/load-macvtap", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, _ = call(Request{Action: "load-macvtap"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("/restart-lightos", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, _ = call(Request{Action: "restart-lightos"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		resp, err := call(Request{Action: "status"})
		if err != nil {
			resp = Response{OK: false, Message: "agent 未就绪: " + err.Error()}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = statusTmpl.Execute(w, resp)
	})
	return mux
}

func RunWeb(listen string) {
	h := webHandler(func(req Request) (Response, error) {
		return CallIPC(socketPath, req)
	})
	log.Printf("web listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, h))
}
