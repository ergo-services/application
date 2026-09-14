package observer

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

const testMount = "/observability"

func mountedObserver(t *testing.T) uint16 {
	t.Helper()

	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	s.StartNode("obs_mounted", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			CreateApp(Options{Host: "localhost", Port: port, Path: testMount}),
		},
	})
	return port
}

func fetchPath(t *testing.T, port uint16, path string) (*http.Response, string) {
	t.Helper()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Get(fmt.Sprintf("http://localhost:%d%s", port, path))
	if err != nil {
		t.Fatalf("get %s: %s", path, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response, string(body)
}

func TestMountedBundleCarriesBase(t *testing.T) {
	port := mountedObserver(t)

	for _, path := range []string{testMount + "/", testMount + "/node/1001/processes"} {
		response, body := fetchPath(t, port, path)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s answered %s", path, response.Status)
		}
		if strings.Contains(body, `<base href="/observability/">`) == false {
			t.Fatalf("%s carries no base tag", path)
		}
	}
}

func TestMountedRootRedirects(t *testing.T) {
	port := mountedObserver(t)

	response, _ := fetchPath(t, port, testMount)
	if response.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("%s answered %s, want a redirect", testMount, response.Status)
	}
	if location := response.Header.Get("Location"); location != testMount+"/" {
		t.Fatalf("redirected to %q", location)
	}
}

func TestMountedObserverServesNothingAtTheRoot(t *testing.T) {
	port := mountedObserver(t)

	for _, path := range []string{"/", "/api/capabilities", "/sse"} {
		response, _ := fetchPath(t, port, path)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s answered %s, want 404", path, response.Status)
		}
	}
}

func TestMountedAPIAnswersUnderThePrefix(t *testing.T) {
	port := mountedObserver(t)

	response, body := fetchPath(t, port, testMount+"/api/capabilities")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("capabilities answered %s", response.Status)
	}
	if strings.Contains(body, `"v"`) == false {
		t.Fatalf("capabilities answered %q", body)
	}
}

func TestRootBundleCarriesBase(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	s.StartNode("obs_root", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			CreateApp(Options{Host: "localhost", Port: port}),
		},
	})

	for _, path := range []string{"/", "/node/1001/processes"} {
		response, body := fetchPath(t, port, path)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s answered %s", path, response.Status)
		}
		if strings.Contains(body, `<base href="/">`) == false {
			t.Fatalf("%s carries no base tag", path)
		}
	}
}

func TestMountPathIsNormalized(t *testing.T) {
	for _, c := range []struct {
		in, want string
		fails    bool
	}{
		{in: "", want: ""},
		{in: "/", want: ""},
		{in: "/observability", want: "/observability"},
		{in: "/observability/", want: "/observability"},
		{in: "/a/b", want: "/a/b"},
		{in: "observability", fails: true},
		{in: "/a?b=1", fails: true},
		{in: "/a#b", fails: true},
		{in: "/a//b", fails: true},
		{in: "/a/../b", fails: true},
	} {
		got, err := normalizeMountPath(c.in)
		if c.fails {
			if err == nil {
				t.Errorf("%q was accepted as %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q refused: %s", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q became %q, want %q", c.in, got, c.want)
		}
	}
}
