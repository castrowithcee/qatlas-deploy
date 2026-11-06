package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The goldens of the Settings area fix its pages, its forms and its questions byte for byte. The masked fields
// of the forms hold typed values; the goldens show them hidden, and goldenScreens fails if one shows.

const settingsRows = 24

func TestGoldenSettingsMenu(t *testing.T) {
	goldenScreens(t, "settings.menu", settingsRows, settingsBackend, toMenu...)
}

func TestGoldenSettingsRepo(t *testing.T) {
	goldenScreens(t, "settings.repo", settingsRows, settingsBackend, toRepo...)
}

func TestGoldenSettingsRepoWithoutRepository(t *testing.T) {
	goldenScreens(t, "settings.repo.none", settingsRows, func() *fakeBackend {
		b := settingsBackend()
		b.status.Engine.Stacks = nil
		b.stacksErr = noRepoError()
		return b
	}, toRepo...)
}

func TestGoldenSettingsRepoForm(t *testing.T) {
	goldenScreens(t, "settings.repo.form", settingsRows, settingsBackend, seq(toRepo, keys("b"), typed(repoURL), keys("tab"), typed(stackDir))...)
}

func TestGoldenSettingsRepoFormProblem(t *testing.T) {
	goldenScreens(t, "settings.repo.problem", settingsRows, settingsBackend, seq(toRepo, keys("b", "f2"))...)
}

func TestGoldenSettingsBindQuestion(t *testing.T) {
	goldenScreens(t, "settings.repo.bind", settingsRows, settingsBackend, seq(toRepo, keys("b"), typed(repoURL), keys("tab"), typed(stackDir), keys("f2"))...)
}

func TestGoldenSettingsFetched(t *testing.T) {
	goldenScreens(t, "settings.repo.fetched", settingsRows, settingsBackend, seq(toRepo, keys("F", "y"))...)
}

func TestGoldenSettingsKeyNone(t *testing.T) {
	goldenScreens(t, "settings.key.none", settingsRows, settingsBackend, toKey...)
}

func TestGoldenSettingsKeyShown(t *testing.T) {
	goldenScreens(t, "settings.key.shown", settingsRows, settingsBackend, seq(toKey, keys("e", "right", "f2", "enter"))...)
}

func TestGoldenSettingsKeyForm(t *testing.T) {
	goldenScreens(t, "settings.key.form", settingsRows, func() *fakeBackend { return keyBackend("/srv/first-example", "/srv/second-example") },
		seq(toKey, keys("e"))...)
}

func TestGoldenSettingsKeyStoreForm(t *testing.T) {
	goldenScreens(t, "settings.key.store", settingsRows, settingsBackend, seq(toKey, keys("e", "f2"), typed(hiddenA))...)
}

func TestGoldenSettingsKeyStoreProblem(t *testing.T) {
	goldenScreens(t, "settings.key.problem", settingsRows, settingsBackend, seq(toKey, keys("e", "f2", "f2"))...)
}

func TestGoldenSettingsKeyReplaceQuestion(t *testing.T) {
	goldenScreens(t, "settings.key.replace", settingsRows, func() *fakeBackend {
		b := keyBackend("/srv/first-example")
		b.key = &control.KeyResult{PublicKey: "ssh-ed25519 AAAAexamplekey qdeploy", Fingerprint: fingerprint}
		return b
	}, seq(toKey, keys("e", "f2"), typed(hiddenA), keys("f2"))...)
}

func TestGoldenSettingsCredentials(t *testing.T) {
	goldenScreens(t, "settings.creds", settingsRows, settingsBackend, toCreds...)
}

func TestGoldenSettingsCredentialsEmpty(t *testing.T) {
	goldenScreens(t, "settings.creds.empty", settingsRows, func() *fakeBackend {
		b := settingsBackend()
		b.creds = nil
		return b
	}, toCreds...)
}

func registryFormKeys() []string {
	return seq(toCreds, keys("a", "down"), typed("Registry.Example.test:5000"), keys("down"), typed("alice"), keys("down"), typed(hiddenA))
}

func TestGoldenSettingsCredentialForm(t *testing.T) {
	goldenScreens(t, "settings.creds.form", settingsRows, settingsBackend, registryFormKeys()...)
}

func TestGoldenSettingsCredentialFormProblem(t *testing.T) {
	goldenScreens(t, "settings.creds.problem", settingsRows+6, settingsBackend, seq(toCreds, keys("a", "f2"))...)
}

func TestGoldenSettingsCredentialQuestion(t *testing.T) {
	goldenScreens(t, "settings.creds.confirm", settingsRows, settingsBackend, seq(registryFormKeys(), keys("f2"))...)
}

func TestGoldenSettingsCredentialDeleteQuestion(t *testing.T) {
	goldenScreens(t, "settings.creds.delete", settingsRows, settingsBackend, seq(toCreds, keys("x"))...)
}

func TestGoldenSettingsWindow(t *testing.T) {
	goldenScreens(t, "settings.window", settingsRows, settingsBackend, toWindow...)
}

func TestGoldenSettingsWindowForm(t *testing.T) {
	goldenScreens(t, "settings.window.form", settingsRows, settingsBackend, seq(toWindow, keys("e"))...)
}

func TestGoldenSettingsWindowQuestion(t *testing.T) {
	goldenScreens(t, "settings.window.confirm", settingsRows, settingsBackend,
		seq(toWindow, keys("e", "tab", "backspace", "backspace", "backspace", "backspace", "backspace"), typed("04:30"), keys("f2"))...)
}

func TestGoldenSettingsNotifications(t *testing.T) {
	goldenScreens(t, "settings.notify", settingsRows, settingsBackend, toNotify...)
}

func TestGoldenSettingsNotificationsEmpty(t *testing.T) {
	goldenScreens(t, "settings.notify.empty", settingsRows, func() *fakeBackend {
		b := settingsBackend()
		b.notify = nil
		return b
	}, toNotify...)
}

func TestGoldenSettingsNotifyForm(t *testing.T) {
	goldenScreens(t, "settings.notify.form", settingsRows, settingsBackend, seq(toNotify, keys("a"), typed("deploy-hook"), keys("down", "down", "space"))...)
}

func webhookFormKeys() []string {
	return seq(toNotify, keys("a"), typed("deploy-hook"), keys("f2"), typed(hiddenA), keys("down"), typed(hiddenB))
}

func TestGoldenSettingsNotifyWebhookForm(t *testing.T) {
	goldenScreens(t, "settings.notify.webhook", settingsRows, settingsBackend, webhookFormKeys()...)
}

func TestGoldenSettingsNotifySMTPForm(t *testing.T) {
	goldenScreens(t, "settings.notify.smtp", settingsRows+8, settingsBackend,
		seq(toNotify, keys("a"), typed("alerts"), keys("down", "right", "right", "right", "f2"), typed("mail.example.test"),
			keys("down", "down", "down", "down"), typed(hiddenA))...)
}

func TestGoldenSettingsNotifyQuestion(t *testing.T) {
	goldenScreens(t, "settings.notify.confirm", settingsRows, settingsBackend, seq(webhookFormKeys(), keys("f2"))...)
}

func TestGoldenSettingsNotifyTestQuestion(t *testing.T) {
	goldenScreens(t, "settings.notify.test", settingsRows, settingsBackend, seq(toNotify, keys("s"))...)
}

func TestGoldenSettingsNotifyRemoveQuestion(t *testing.T) {
	goldenScreens(t, "settings.notify.remove", settingsRows, settingsBackend, seq(toNotify, keys("down", "x"))...)
}

func TestGoldenSettingsDiscardQuestion(t *testing.T) {
	goldenScreens(t, "settings.discard", settingsRows, settingsBackend, seq(toRepo, keys("b"), typed(repoURL), keys("esc"))...)
}

func TestGoldenDetailDiscardQuestion(t *testing.T) {
	goldenScreens(t, "detail.discard", detailRows, settingsBackend, seq(toSecretForm, typed("SERVICE_TOKEN"), keys("down"), typed(hiddenA), keys("esc"))...)
}
