package weread

import (
	"encoding/json"
	"os"
	"testing"
)

type vectorFile struct {
	WereadE []struct {
		In   string `json:"in"`
		Want string `json:"want"`
	} `json:"weread_e"`
	WereadSign []struct {
		Query string `json:"query"`
		Want  string `json:"want"`
	} `json:"weread_sign"`
	SortedQuery []struct {
		Params map[string]any `json:"params"`
		Want   string         `json:"want"`
	} `json:"sorted_query"`
	Decode []struct {
		Encoded string `json:"encoded"`
		Want    string `json:"want"`
	} `json:"decode"`
	Shards struct {
		E0   string `json:"e0"`
		E1   string `json:"e1"`
		E3   string `json:"e3"`
		Want string `json:"want"`
	} `json:"shards"`
	SwapPositions []struct {
		Encoded string `json:"encoded"`
		Want    []int  `json:"want"`
	} `json:"swap_positions"`
}

func loadVectors(t *testing.T) *vectorFile {
	t.Helper()
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(data, &vf); err != nil {
		t.Fatal(err)
	}
	return &vf
}

func TestWereadEVectors(t *testing.T) {
	known := map[string]string{
		"43208843": "c9c321c07293508bc9c79df",
		"2":        "c81322c012c81e728d9d180",
		"119":      "07e323f027707e1cd7dc674",
	}
	for in, want := range known {
		if got := WereadE(in); got != want {
			t.Errorf("WereadE(%q) = %q, want %q", in, got, want)
		}
	}
	vf := loadVectors(t)
	for _, tc := range vf.WereadE {
		if got := WereadE(tc.In); got != tc.Want {
			t.Errorf("WereadE(%q) = %q, want %q", tc.In, got, tc.Want)
		}
	}
}

func TestWereadSignVectors(t *testing.T) {
	vf := loadVectors(t)
	for _, tc := range vf.WereadSign {
		if got := WereadSign(tc.Query); got != tc.Want {
			t.Errorf("WereadSign(%q) = %q, want %q", tc.Query, got, tc.Want)
		}
	}
}

func TestSortedQueryVectors(t *testing.T) {
	vf := loadVectors(t)
	for _, tc := range vf.SortedQuery {
		if got := SortedQuery(tc.Params); got != tc.Want {
			t.Errorf("SortedQuery(%v) = %q, want %q", tc.Params, got, tc.Want)
		}
	}
}

func TestDecodeEncodedPayloadVectors(t *testing.T) {
	vf := loadVectors(t)
	for _, tc := range vf.Decode {
		if got := decodeEncodedPayload(tc.Encoded); got != tc.Want {
			t.Errorf("decodeEncodedPayload mismatch:\n got %q\nwant %q", got, tc.Want)
		}
	}
}

func TestDecodeShardsFixture(t *testing.T) {
	vf := loadVectors(t)
	got, err := decodeShards(vf.Shards.E0, vf.Shards.E1, vf.Shards.E3)
	if err != nil {
		t.Fatal(err)
	}
	if got != vf.Shards.Want {
		t.Errorf("decodeShards mismatch:\n got %q\nwant %q", got[:min(80, len(got))], vf.Shards.Want[:min(80, len(vf.Shards.Want))])
	}
}

func TestSwapPositionsVectors(t *testing.T) {
	vf := loadVectors(t)
	for _, tc := range vf.SwapPositions {
		got := swapPositions(tc.Encoded)
		if len(got) != len(tc.Want) {
			t.Errorf("swapPositions(%q) = %v, want %v", tc.Encoded, got, tc.Want)
			continue
		}
		for i := range got {
			if got[i] != tc.Want[i] {
				t.Errorf("swapPositions(%q) = %v, want %v", tc.Encoded, got, tc.Want)
				break
			}
		}
	}
}

func TestCheckedBody(t *testing.T) {
	if got, err := checkedBody(""); err != nil || got != "" {
		t.Errorf("empty: got %q err %v", got, err)
	}
	good := shardWithMD5("payload-body")
	got, err := checkedBody(good)
	if err != nil || got != "payload-body" {
		t.Errorf("good: got %q err %v", got, err)
	}
	if _, err := checkedBody("00000000000000000000000000000000tampered"); err == nil {
		t.Error("expected MD5 mismatch error")
	}
}
