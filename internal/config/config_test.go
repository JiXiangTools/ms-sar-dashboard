package config

import "testing"

func TestAlertConfigurationIsOptionalAndCanBeOverridden(t *testing.T) {
	cfg, err := Load("../../configs/dev.yaml", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Alert.Enabled {
		t.Fatal("development alerts should default off")
	}
	cfg, err = Load("../../configs/test.yaml", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Alert.Enabled {
		t.Fatal("test alerts should be enabled")
	}
	t.Setenv("MSSAR_ALERT_ENABLED", "false")
	cfg, err = Load("../../configs/test.yaml", "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Alert.Enabled {
		t.Fatal("environment must disable configured alerts")
	}
}

func TestAlertWebhookConfiguration(t *testing.T) {
	t.Setenv("MSSAR_ALERT_ENABLED", "true")
	t.Setenv("MSSAR_ALERT_FEISHU_WEBHOOK_URL", "")
	cfg, err := Load("../../configs/test.yaml", "test")
	if err != nil || cfg.Alert.FeishuWebhookURL != "" {
		t.Fatalf("empty webhook must disable sending: %v", err)
	}
	t.Setenv("MSSAR_ALERT_FEISHU_WEBHOOK_URL", "http://insecure.example/hook")
	if _, err := Load("../../configs/test.yaml", "test"); err == nil {
		t.Fatal("insecure webhook accepted")
	}
	t.Setenv("MSSAR_ALERT_FEISHU_WEBHOOK_URL", "https://open.feishu.cn/open-apis/bot/v2/hook/test-override")
	cfg, err = Load("../../configs/test.yaml", "test")
	if err != nil || cfg.Alert.FeishuWebhookURL != "https://open.feishu.cn/open-apis/bot/v2/hook/test-override" {
		t.Fatal("webhook environment override failed")
	}
}
