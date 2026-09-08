package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

type fakeReadFinder struct {
	ids        map[string][]uint32
	raw        map[string][]byte
	searchErr  map[string]error
	defaultErr error
	searched   []string
}

func (f *fakeReadFinder) SearchMessageIDs(_ context.Context, folder, id string) ([]uint32, error) {
	f.searched = append(f.searched, folder+"="+id)
	if err := f.searchErr[folder]; err != nil {
		return nil, err
	}
	if f.defaultErr != nil {
		return nil, f.defaultErr
	}
	return f.ids[folder], nil
}

func (f *fakeReadFinder) FetchRaw(_ context.Context, folder string, uid uint32) ([]byte, error) {
	return f.raw[folder+":"+itoa(uid)], nil
}

func itoa(n uint32) string {
	return strconv.FormatUint(uint64(n), 10)
}

func readRaw(id, body string) []byte {
	return []byte("From: Alice <alice@example.com>\r\n" +
		"To: Agent <agent@example.com>\r\n" +
		"Cc: copy@example.com\r\n" +
		"Date: Mon, 07 Sep 2026 12:00:00 +0000\r\n" +
		"Subject: Test subject\r\n" +
		"Message-ID: <" + id + ">\r\n" +
		"In-Reply-To: <parent@example.com>\r\n" +
		"References: <root@example.com> <parent@example.com>\r\n\r\n" + body + "\r\n")
}

func TestParseAgentReadArgsAllowsFlagsAfterLink(t *testing.T) {
	opts, err := parseAgentReadArgs([]string{"neomd://mid/id%40example.com", "--folder", "Sent", "--account=Work", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.messageID == "" || opts.folder != "Sent" || opts.account != "Work" || !opts.json {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestSearchAgentMessageFolderHintFirst(t *testing.T) {
	folders := testFolders()
	folders.Sent = "Sent"
	finder := &fakeReadFinder{
		ids: map[string][]uint32{"Inbox": {1}, "Sent": {2}},
		raw: map[string][]byte{"Sent:2": readRaw("target@example.com", "hello")},
	}
	var out, errOut bytes.Buffer
	code := runAgentRead(context.Background(), folders, []readClient{{account: "Work", client: finder}}, agentReadOpts{
		messageID: "<target@example.com>", folder: "Sent", json: true,
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if len(finder.searched) < 1 || !strings.HasPrefix(finder.searched[0], "Sent=") {
		t.Fatalf("search order = %v", finder.searched)
	}
	var result agentReadResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Found || result.Body != "hello" || result.Account != "Work" || result.Folder != "Sent" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunAgentReadUsesURIHostFolderAndStdinBatch(t *testing.T) {
	folders := testFolders()
	folders.Sent = "Sent"
	finder := &fakeReadFinder{
		ids: map[string][]uint32{"Sent": {2}},
		raw: map[string][]byte{"Sent:2": readRaw("target@example.com", "batch body")},
	}
	var out, errOut bytes.Buffer
	code := runAgentRead(context.Background(), folders, []readClient{{account: "Personal", client: finder}}, agentReadOpts{json: true},
		strings.NewReader("neomd://mid/target%40example.com?folder=Sent\n"), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q searches=%v", code, out.String(), errOut.String(), finder.searched)
	}
	var result agentReadResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Body != "batch body" {
		t.Fatalf("output=%q err=%v", out.String(), err)
	}
}

func TestRunAgentReadNotFoundReportsFolders(t *testing.T) {
	finder := &fakeReadFinder{ids: map[string][]uint32{}, searchErr: map[string]error{}}
	var out, errOut bytes.Buffer
	code := runAgentRead(context.Background(), testFolders(), []readClient{{account: "Personal", client: finder}}, agentReadOpts{messageID: "missing@example.com"},
		strings.NewReader(""), &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "message not found") || !strings.Contains(errOut.String(), "Personal:Inbox") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestRunAgentReadSearchErrorIsConfigOrAuthFailure(t *testing.T) {
	finder := &fakeReadFinder{defaultErr: errors.New("authentication failed")}
	var out, errOut bytes.Buffer
	code := runAgentRead(context.Background(), testFolders(), []readClient{{account: "Personal", client: finder}}, agentReadOpts{messageID: "missing@example.com"},
		strings.NewReader(""), &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "authentication failed") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}
