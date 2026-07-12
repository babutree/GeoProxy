package storage

import "testing"

// TestManualRegionAlpha2Validation 覆盖 BUG-64：手动节点 region 后端兜底校验。
// region 必须是 2 位 alpha（存小写），非法值规范化为 ""（未知地域/自动），
// 而非报错拒绝——地域是可选辅助信息。此测试同时覆盖 AddManualProxy 与
// UpdateProxyRegionByID 两个用户可写入口。
func TestManualRegionAlpha2Validation(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		// 负向：非法 region 必须落库为空串
		{name: "xss_script", input: "<script>alert(1)</script>", want: ""},
		{name: "three_letters_USA", input: "USA", want: ""},
		{name: "one_letter", input: "u", want: ""},
		{name: "digits", input: "12", want: ""},
		{name: "two_digits_mixed", input: "u1", want: ""},
		{name: "empty", input: "", want: ""},
		{name: "whitespace_only", input: "   ", want: ""},
		{name: "alpha3_lower", input: "usa", want: ""},
		{name: "with_symbols", input: "u$", want: ""},
		// 正向：合法 alpha2 规范化为小写
		{name: "lower_jp", input: "jp", want: "jp"},
		{name: "upper_JP", input: "JP", want: "jp"},
		{name: "padded_us", input: " us ", want: "us"},
		{name: "upper_HK", input: "HK", want: "hk"},
	}

	for _, tc := range cases {
		t.Run("Add/"+tc.name, func(t *testing.T) {
			store := newTestStorage(t)
			addr := "add-" + tc.name + ":8080"
			if err := store.AddManualProxy(addr, "http", tc.input, "note"); err != nil {
				t.Fatalf("AddManualProxy(region=%q) error = %v", tc.input, err)
			}
			proxy, err := store.GetProxyByAddress(addr)
			if err != nil {
				t.Fatalf("GetProxyByAddress() error = %v", err)
			}
			if proxy.Region != tc.want {
				t.Fatalf("AddManualProxy region = %q, want %q (input %q)", proxy.Region, tc.want, tc.input)
			}
			// region_source 语义：非空 region 为 manual，空 region 回退 auto
			wantSource := "auto"
			if tc.want != "" {
				wantSource = "manual"
			}
			if proxy.RegionSource != wantSource {
				t.Fatalf("AddManualProxy region_source = %q, want %q (input %q)", proxy.RegionSource, wantSource, tc.input)
			}
		})

		t.Run("UpdateByID/"+tc.name, func(t *testing.T) {
			store := newTestStorage(t)
			addr := "upd-" + tc.name + ":8080"
			// 先以合法 region 建节点，再用测试输入覆盖，验证覆盖后的落库值
			if err := store.AddManualProxy(addr, "http", "us", "note"); err != nil {
				t.Fatalf("AddManualProxy(seed) error = %v", err)
			}
			seed, err := store.GetProxyByAddress(addr)
			if err != nil {
				t.Fatalf("GetProxyByAddress(seed) error = %v", err)
			}
			if err := store.UpdateProxyRegionByID(seed.ID, tc.input, true); err != nil {
				t.Fatalf("UpdateProxyRegionByID(region=%q) error = %v", tc.input, err)
			}
			proxy, err := store.GetProxyByID(seed.ID)
			if err != nil {
				t.Fatalf("GetProxyByID() error = %v", err)
			}
			if proxy.Region != tc.want {
				t.Fatalf("UpdateProxyRegionByID region = %q, want %q (input %q)", proxy.Region, tc.want, tc.input)
			}
		})
	}
}

// TestValidatorRegionWriteNotRegressed 确认 BUG-64 的修复未回归验证器写入路径。
// 验证器通过 updateExitInfoWhere -> regionFromExitLocation 写 region，不经过
// normalizeManualRegion；合法 alpha2 小写国家码应仍能正常写入。
func TestValidatorRegionWriteNotRegressed(t *testing.T) {
	store := newTestStorage(t)
	insertTestSubscription(t, store, 1, "active")
	insertProxyWithRegionSource(t, store, "auto:8080", "", "auto")

	if err := store.UpdateExitInfo("auto:8080", "8.8.8.8", "US Mountain View", 120, -1, "", -1, ""); err != nil {
		t.Fatalf("UpdateExitInfo() error = %v", err)
	}
	proxy, err := store.GetProxyByAddress("auto:8080")
	if err != nil {
		t.Fatalf("GetProxyByAddress() error = %v", err)
	}
	if proxy.Region != "us" {
		t.Fatalf("validator-written region = %q, want us", proxy.Region)
	}
}

// TestNormalizeManualRegionUnit 直接对规范化函数做单元级正向/负向断言。
func TestNormalizeManualRegionUnit(t *testing.T) {
	valid := map[string]string{
		"jp":   "jp",
		"JP":   "jp",
		" us ": "us",
		"Hk":   "hk",
	}
	for in, want := range valid {
		if got := normalizeManualRegion(in); got != want {
			t.Errorf("normalizeManualRegion(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := []string{"<script>alert(1)</script>", "USA", "u", "12", "u1", "", "   ", "usa", "u$", "日本"}
	for _, in := range invalid {
		if got := normalizeManualRegion(in); got != "" {
			t.Errorf("normalizeManualRegion(%q) = %q, want \"\"", in, got)
		}
	}
}
