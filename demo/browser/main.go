// Command browser records the browser part of the optimus demo: it drives the
// web dashboard in headless Chrome (with a visible cursor and typed input),
// captures frames, and saves README screenshots along the way.
//
// usage: go run . -url URL -out DIR [-chrome PATH]
//
// Frames land in DIR/desktop and DIR/mobile with a frames.txt concat list
// (ffmpeg concat demuxer format) each; screenshots in DIR/shots.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// recorder grabs JPEG frames from a page at a steady rate until stopped.
type recorder struct {
	page   *rod.Page
	dir    string
	mu     sync.Mutex
	frames []frame
	stop   chan struct{}
	done   chan struct{}
}

type frame struct {
	file string
	at   time.Time
}

func record(p *rod.Page, dir string, fps int) *recorder {
	os.MkdirAll(dir, 0o755)
	r := &recorder{page: p, dir: dir, stop: make(chan struct{}), done: make(chan struct{})}
	q := 85
	go func() {
		defer close(r.done)
		tick := time.NewTicker(time.Second / time.Duration(fps))
		defer tick.Stop()
		for i := 0; ; i++ {
			select {
			case <-r.stop:
				return
			case <-tick.C:
			}
			at := time.Now()
			img, err := proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatJpeg, Quality: &q}.Call(p)
			if err != nil {
				continue
			}
			f := filepath.Join(dir, fmt.Sprintf("f%05d.jpg", i))
			os.WriteFile(f, img.Data, 0o644)
			r.mu.Lock()
			r.frames = append(r.frames, frame{f, at})
			r.mu.Unlock()
		}
	}()
	return r
}

// finish writes an ffmpeg concat list that replays frames with their real timing.
func (r *recorder) finish() {
	close(r.stop)
	<-r.done
	f, _ := os.Create(filepath.Join(r.dir, "frames.txt"))
	defer f.Close()
	for i, fr := range r.frames {
		d := 1.0 / 12
		if i+1 < len(r.frames) {
			d = r.frames[i+1].at.Sub(fr.at).Seconds()
		}
		fmt.Fprintf(f, "file '%s'\nduration %.4f\n", filepath.Base(fr.file), d)
	}
	if n := len(r.frames); n > 0 {
		fmt.Fprintf(f, "file '%s'\n", filepath.Base(r.frames[n-1].file))
	}
	fmt.Printf("%s: %d frames\n", r.dir, len(r.frames))
}

const cursorJS = `() => {
  if (document.getElementById('demo-cursor')) return;
  const c = document.createElement('div');
  c.id = 'demo-cursor';
  c.innerHTML = '<svg width="22" height="22" viewBox="0 0 24 24"><path d="M3 2l7 19 2.5-7.5L20 11z" fill="#fff" stroke="#11111b" stroke-width="1.6" stroke-linejoin="round"/></svg>';
  Object.assign(c.style, {position:'fixed', left:'50%', top:'60%', zIndex: 99999, pointerEvents:'none',
    transition:'left .55s cubic-bezier(.4,0,.2,1), top .55s cubic-bezier(.4,0,.2,1)', filter:'drop-shadow(0 2px 3px rgba(0,0,0,.5))'});
  document.documentElement.appendChild(c);
  const s = document.createElement('style');
  s.textContent = '.demo-ripple{position:fixed;width:34px;height:34px;margin:-17px 0 0 -17px;border-radius:50%;border:2px solid #89b4fa;z-index:99998;pointer-events:none;animation:demo-r .45s ease-out forwards}@keyframes demo-r{from{transform:scale(.3);opacity:1}to{transform:scale(1.4);opacity:0}}';
  document.head.appendChild(s);
}`

type demo struct {
	p *rod.Page
}

func (d demo) cursor() { d.p.MustEval(cursorJS) }

// moveTo glides the fake cursor to the centre of an element.
func (d demo) moveTo(sel string) *rod.Element {
	el := d.p.MustElement(sel)
	el.MustEval(`() => this.scrollIntoView({block: "nearest", inline: "center"})`)
	d.cursor()
	d.p.MustEval(`(sel) => { const r = document.querySelector(sel).getBoundingClientRect();
	  const c = document.getElementById('demo-cursor'); c.style.left = (r.left + r.width/2) + 'px'; c.style.top = (r.top + r.height/2) + 'px'; }`, sel)
	time.Sleep(650 * time.Millisecond)
	return el
}

func (d demo) click(sel string) {
	el := d.moveTo(sel)
	d.p.MustEval(`(sel) => { const r = document.querySelector(sel).getBoundingClientRect(); const e = document.createElement('div');
	  e.className = 'demo-ripple'; e.style.left = (r.left + r.width/2) + 'px'; e.style.top = (r.top + r.height/2) + 'px';
	  document.documentElement.appendChild(e); setTimeout(() => e.remove(), 500); }`, sel)
	// a real click, unless the element isn't fully clickable (e.g. a card half
	// off-screen in the phone layout's swipe row): then a programmatic one
	if err := el.Timeout(3*time.Second).Click(proto.InputMouseButtonLeft, 1); err != nil {
		el.MustEval(`() => this.click()`)
	}
	time.Sleep(250 * time.Millisecond)
}

func (d demo) typeInto(sel, text string) {
	d.click(sel)
	for _, r := range text {
		d.p.MustInsertText(string(r))
		time.Sleep(38 * time.Millisecond)
	}
}

func wait(s float64) { time.Sleep(time.Duration(s * float64(time.Second))) }

func shot(p *rod.Page, path string) {
	os.WriteFile(path, p.MustScreenshot(), 0o644)
	fmt.Println("screenshot", path)
}

func main() {
	url := flag.String("url", "", "dashboard URL with token")
	out := flag.String("out", "build", "output dir")
	chrome := flag.String("chrome", "", "Chrome/Chromium binary (default: rod's)")
	w := flag.Int("w", 1600, "width")
	h := flag.Int("h", 920, "height")
	flag.Parse()
	shots := filepath.Join(*out, "shots")
	os.MkdirAll(shots, 0o755)

	l := launcher.New().Headless(true).UserDataDir(filepath.Join(*out, "chrome-profile")).Set("hide-scrollbars")
	if *chrome != "" {
		l = l.Bin(*chrome)
	}
	b := rod.New().ControlURL(l.MustLaunch()).MustConnect()
	defer b.MustClose()

	// ---- desktop
	p := b.MustPage("")
	p.MustSetViewport(*w, *h, 1, false)
	p.MustNavigate(*url).MustWaitLoad()
	wait(2.5)
	d := demo{p}
	d.cursor()
	rec := record(p, filepath.Join(*out, "desktop"), 12)

	wait(2.5) // the fleet: live terminal of a busy agent
	shot(p, filepath.Join(shots, "web-fleet.png"))
	d.click(`#windows .win:nth-child(2)`) // codex is waiting for approval
	wait(2.2)
	d.click(`#keys [data-key="1"]`)
	d.click(`#keys [data-key="Enter"]`)
	wait(2.5)

	d.typeInto("#prompt", "run the test suite and summarize any failures")
	d.click("#broadcast")
	d.click("#prompt-form button.primary")
	wait(2.5)
	d.click(`#windows .win:nth-child(3)`)
	wait(3)

	d.click("#new-btn")
	wait(0.8)
	d.click(`#new-agents [data-agent="codex"]`)
	d.click(`#new-recent [data-dir$="/web"]`)
	d.typeInto("#new-prompt", "add e2e tests for the passkey sign-in flow")
	wait(0.6)
	shot(p, filepath.Join(shots, "web-new-session.png"))
	d.click("#new-go")
	wait(4.5)

	d.click(`.tabs [data-view="sessions"]`)
	wait(1.2)
	d.typeInto("#s-q", "rate")
	wait(1)
	shot(p, filepath.Join(shots, "web-sessions.png"))
	d.click("#s-body tr td.title")
	wait(2.5)
	shot(p, filepath.Join(shots, "web-transcript.png"))
	d.click("#tr-handoff")
	wait(1.2)
	shot(p, filepath.Join(shots, "web-handoff.png"))
	d.click(`#handoff-targets [data-t="new:codex"]`)
	wait(6)

	d.click(`.tabs [data-view="usage"]`)
	wait(3.5)
	shot(p, filepath.Join(shots, "web-usage.png"))
	rec.finish()

	// ---- phone
	m := b.MustPage("")
	m.MustEmulate(devices.IPhoneX)
	m.MustNavigate(*url).MustWaitLoad()
	wait(2.5)
	md := demo{m}
	md.cursor()
	mrec := record(m, filepath.Join(*out, "mobile"), 12)
	wait(2)
	md.click(`#windows .win:nth-child(2)`)
	wait(2)
	shot(m, filepath.Join(shots, "mobile-fleet.png"))
	md.typeInto("#prompt", "looks good, ship it")
	md.click("#prompt-form button.primary")
	wait(3)
	md.click(`#bottom-nav [data-view="sessions"]`)
	wait(2.2)
	md.click(`#bottom-nav [data-view="usage"]`)
	wait(2.5)
	shot(m, filepath.Join(shots, "mobile-usage.png"))
	mrec.finish()
}
