//go:build android

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// 虚拟显示屏 (低帧率版): screencap 轮询 MJPEG 推流 + 触控转发

var vscreen struct {
	sync.Mutex
	running bool
}

// vscreen_start: 启动 MJPEG 服务 (screencap 轮询 ~5fps)
func mVscreenStart(c *conn, m Msg) {
	vscreen.Lock()
	if vscreen.running {
		vscreen.Unlock()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "已在运行"})
		return
	}
	vscreen.running = true
	vscreen.Unlock()
	go startMjpegServer()
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true),
		Stdout: "虚拟显示屏已启动: http://<设备IP>:27184/  (~5fps, 注入版后续优化)"})
}

// vscreen_stop
func mVscreenStop(c *conn, m Msg) {
	vscreen.Lock()
	vscreen.running = false
	vscreen.Unlock()
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "已停止"})
}

// captureFrame: screencap 截屏 → 缩放 → JPEG (双缓冲流水线)
func captureFrame() ([]byte, error) {
	// screencap stdout 直出 (130ms)
	rc, out, se := runCmdCapture("screencap", "/dev/stdout")
	if rc != 0 {
		return nil, fmt.Errorf("screencap 失败: %s", se)
	}
	raw := out
	w, h := 1216, 2640
	if len(raw) < w*h*4 {
		return nil, fmt.Errorf("raw 尺寸异常: %d", len(raw))
	}
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(src.Pix, raw[:w*h*4])
	// 缩放到 1/2 (608x1320), 最近邻
	dw, dh := 608, 1320
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := y * h / dh
		for x := 0; x < dw; x++ {
			sxi := x * w / dw
			si := (sy*w + sxi) * 4
			di := (y*dw + x) * 4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 60})
	return buf.Bytes(), nil
}

// startMjpegServer: HTTP MJPEG 流 + 触控转发 + 控制页面 (双缓冲流水线)
func startMjpegServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/mjpeg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		for {
			vscreen.Lock()
			running := vscreen.running
			vscreen.Unlock()
			if !running {
				return
			}
			frame, err := captureFrame()
			if err != nil {
				continue
			}
			writeMjpegFrame(w, frame)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	// 触控转发: POST /touch {"type":"tap|swipe","x":..,"y":..,"x2":..,"y2":..}
	mux.HandleFunc("/touch", func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		s := string(b[:n])
		typ := jsonGetString(s, "type")
		x := jsonGetFloat(s, "x")
		y := jsonGetFloat(s, "y")
		x2 := jsonGetFloat(s, "x2")
		y2 := jsonGetFloat(s, "y2")
		if typ == "tap" {
			runCmd("input", "tap", fmt.Sprintf("%d", int(x)), fmt.Sprintf("%d", int(y)))
		} else if typ == "swipe" {
			runCmd("input", "swipe", fmt.Sprintf("%d", int(x)), fmt.Sprintf("%d", int(y)),
				fmt.Sprintf("%d", int(x2)), fmt.Sprintf("%d", int(y2)), "200")
		}
		fmt.Fprint(w, "ok")
	})
	// 控制页面
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, htmlPage)
	})
	http.ListenAndServe(":27184", mux)
}

func writeMjpegFrame(w http.ResponseWriter, data []byte) {
	fmt.Fprint(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: ")
	fmt.Fprintf(w, "%d\r\n\r\n", len(data))
	w.Write(data)
	fmt.Fprint(w, "\r\n")
}

// runCmdCapture: 执行命令并捕获 stdout 二进制
func runCmdCapture(name string, args ...string) (int, []byte, string) {
	cmd := exec.Command(name, args...)
	var so bytes.Buffer
	var se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	rc := 0
	if err != nil {
		rc = -1
	}
	return rc, so.Bytes(), se.String()
}

// runCmdCaptureRaw: screencap 专用 (大输出)
func runCmdCaptureRaw(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var so bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return so.Bytes(), nil
}

// encodeJpeg: RGBA raw → 缩放 → JPEG
func encodeJpeg(raw []byte, w, h, dw, dh int) []byte {
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(src.Pix, raw[:w*h*4])
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := y * h / dh
		for x := 0; x < dw; x++ {
			sxi := x * w / dw
			si := (sy*w + sxi) * 4
			di := (y*dw + x) * 4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 60})
	return buf.Bytes()
}

func jsonGetString(s, key string) string {
	idx := strings.Index(s, "\""+key+"\"")
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key)+2:]
	rest = strings.TrimLeft(rest, " :\"")
	end := strings.Index(rest, "\"")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func jsonGetFloat(s, key string) float64 {
	idx := strings.Index(s, "\""+key+"\"")
	if idx < 0 {
		return 0
	}
	rest := s[idx+len(key)+2:]
	rest = strings.TrimLeft(rest, " :")
	var f float64
	fmt.Sscanf(rest, "%f", &f)
	return f
}

const htmlPage = `<!DOCTYPE html>
<html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, user-scalable=no">
<title>devctl 虚拟显示屏</title>
<style>
html,body{margin:0;padding:0;background:#000;height:100%;overflow:hidden;touch-action:none}
#screen{width:100vw;height:100vh;object-fit:contain;background:#111}
</style></head>
<body>
<img id="screen" src="/mjpeg">
<script>
const img = document.getElementById('screen');
const W = 1216, H = 2640; // 设备分辨率
let sx = 0, sy = 0, moving = false, lastX = 0, lastY = 0;

function send(obj) {
  fetch('/touch', {method:'POST', body: JSON.stringify(obj)});
}
function scaleTo(e) {
  const rect = img.getBoundingClientRect();
  const scale = Math.min(rect.width/W, rect.height/H);
  const offX = (rect.width - W*scale)/2, offY = (rect.height - H*scale)/2;
  return {x: Math.round((e.clientX-rect.left-offX)/scale),
          y: Math.round((e.clientY-rect.top-offY)/scale)};
}
img.addEventListener('touchstart', e => {
  e.preventDefault();
  const p = scaleTo(e.touches[0]);
  sx = p.x; sy = p.y; lastX = p.x; lastY = p.y;
  moving = false;
  send({type:'tap', x:p.x, y:p.y});
  setTimeout(()=>{moving = true;}, 200); // 200ms 后移动算 swipe
}, {passive:false});
img.addEventListener('touchmove', e => {
  e.preventDefault();
  if (!moving) return;
  const p = scaleTo(e.touches[0]);
  lastX = p.x; lastY = p.y;
}, {passive:false});
img.addEventListener('touchend', e => {
  if (moving) {
    send({type:'swipe', x:sx, y:sy, x2:lastX, y2:lastY});
  }
  moving = false;
});
// 鼠标兼容
img.addEventListener('mousedown', e => {
  const p = scaleTo(e); sx=p.x; sy=p.y; lastX=p.x; lastY=p.y; moving=false;
  send({type:'tap', x:p.x, y:p.y});
  setTimeout(()=>{moving=true;}, 200);
});
img.addEventListener('mousemove', e => {
  if (!moving) return;
  const p = scaleTo(e); lastX=p.x; lastY=p.y;
});
img.addEventListener('mouseup', () => {
  if (moving) send({type:'swipe', x:sx, y:sy, x2:lastX, y2:lastY});
  moving = false;
});
</script>
</body></html>`
