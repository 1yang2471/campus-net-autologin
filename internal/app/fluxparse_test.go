package app

import "testing"

// 行为：userflux 表格按相邻单元格成对解析——参考校是 2 列，异校可能是
// 4 列（键/值/键/值）；旧正则只抓前两格，导致面板只剩「用户名称」。
func TestParseFluxTableTwoCells(t *testing.T) {
	body := `<table><tr><td>用户名称</td><td>23jszeng</td></tr>` +
		`<tr><td>当天流量</td><td>2.40006G</td></tr></table>`
	m := parseFluxTable(body)
	if m["用户名称"] != "23jszeng" || m["当天流量"] != "2.40006G" {
		t.Errorf("2 列表格解析错: %v", m)
	}
}

func TestParseFluxTableFourCells(t *testing.T) {
	body := `<tr><td>用户名称：</td><td>23jszeng</td><td>当天流量</td><td>2.4G</td></tr>`
	m := parseFluxTable(body)
	if m["用户名称"] != "23jszeng" || m["当天流量"] != "2.4G" {
		t.Errorf("4 列表格应配对解析，实际 %v", m)
	}
}

// 行为：表头不是数据——<th> 行不得被当成键值对（否则会污染 flux 表，
// 甚至让「空表判死」逻辑拿到假数据）。
func TestParseFluxTableIgnoresHeader(t *testing.T) {
	body := `<tr><th>项目</th><th>值</th></tr>` +
		`<tr><td>当天流量</td><td>2.4G</td></tr>`
	m := parseFluxTable(body)
	if _, ok := m["项目"]; ok {
		t.Errorf("表头不得进 flux 表: %v", m)
	}
	if m["当天流量"] != "2.4G" {
		t.Errorf("数据行应保留: %v", m)
	}
}
