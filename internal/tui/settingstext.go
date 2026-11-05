package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the Settings area. Technical words of the service (types, events, commands) stay English in
// every language.
const (
	textAreaSettings          kittui.TextID = "area.settings"
	textKeyNext               kittui.TextID = "key.next"
	textKeyPrev               kittui.TextID = "key.prev"
	textKeySave               kittui.TextID = "key.save"
	textKeyBind               kittui.TextID = "key.bind"
	textKeyFetch              kittui.TextID = "key.fetch"
	textKeyGenerate           kittui.TextID = "key.generate"
	textKeyAdd                kittui.TextID = "key.add"
	textKeyRemove             kittui.TextID = "key.remove"
	textKeyEdit               kittui.TextID = "key.edit"
	textKeyTest               kittui.TextID = "key.test"
	textKeyOpen               kittui.TextID = "key.open"
	textKeyCancelForm         kittui.TextID = "key.cancelform"
	textColSetting            kittui.TextID = "col.setting"
	textColAbout              kittui.TextID = "col.about"
	textColName               kittui.TextID = "col.name"
	textColModified           kittui.TextID = "col.modified"
	textColType               kittui.TextID = "col.type"
	textColEvents             kittui.TextID = "col.events"
	textColTarget             kittui.TextID = "col.target"
	textPageRepo              kittui.TextID = "page.repo"
	textPageKey               kittui.TextID = "page.key"
	textPageCreds             kittui.TextID = "page.creds"
	textPageWindow            kittui.TextID = "page.window"
	textPageNotify            kittui.TextID = "page.notify"
	textAboutRepo             kittui.TextID = "about.repo"
	textAboutKey              kittui.TextID = "about.key"
	textAboutCreds            kittui.TextID = "about.creds"
	textAboutWindow           kittui.TextID = "about.window"
	textAboutNotify           kittui.TextID = "about.notify"
	textCredsEmpty            kittui.TextID = "creds.empty"
	textNotifyEmpty           kittui.TextID = "notify.empty"
	textEventsAll             kittui.TextID = "events.all"
	textSettingDoneTitle      kittui.TextID = "setting.done.title"
	textDoneBound             kittui.TextID = "done.bound"
	textDetailHead            kittui.TextID = "detail.head"
	textDetailRemote          kittui.TextID = "detail.remote"
	textDetailCloned          kittui.TextID = "detail.cloned"
	textDoneFetchedCurrent    kittui.TextID = "done.fetched.current"
	textDoneFetchedNew        kittui.TextID = "done.fetched.new"
	textDoneGenerated         kittui.TextID = "done.generated"
	textDoneWindow            kittui.TextID = "done.window"
	textDoneAdded             kittui.TextID = "done.added"
	textDoneTested            kittui.TextID = "done.tested"
	textDoneStored            kittui.TextID = "done.stored"
	textDoneDeleted           kittui.TextID = "done.deleted"
	textDoneRemoved           kittui.TextID = "done.removed"
	textFormRetry             kittui.TextID = "form.retry"
	textFormFix               kittui.TextID = "form.fix"
	textYes                   kittui.TextID = "yes"
	textNo                    kittui.TextID = "no"
	textKeyExists             kittui.TextID = "key.exists"
	textChooseOne             kittui.TextID = "choose.one"
	textAskFetch              kittui.TextID = "ask.fetch"
	textWhatFetch             kittui.TextID = "what.fetch"
	textAskGenerate           kittui.TextID = "ask.generate"
	textWhatGenerate          kittui.TextID = "what.generate"
	textAskCredDelete         kittui.TextID = "ask.creddelete"
	textWhatCredDelete        kittui.TextID = "what.creddelete"
	textAskNotifyRemove       kittui.TextID = "ask.notifyremove"
	textWhatNotifyRemove      kittui.TextID = "what.notifyremove"
	textAskNotifyTest         kittui.TextID = "ask.notifytest"
	textWhatNotifyTest        kittui.TextID = "what.notifytest"
	textAskBind               kittui.TextID = "ask.bind"
	textAskFolderCreate       kittui.TextID = "ask.foldercreate"
	textWhatFolderCreate      kittui.TextID = "what.foldercreate"
	textProblemDirRequired    kittui.TextID = "problem.dirrequired"
	textWhatBind              kittui.TextID = "what.bind"
	textWhatBindPinned        kittui.TextID = "what.bindpinned"
	textDetailHost            kittui.TextID = "detail.host"
	textDetailKeyType         kittui.TextID = "detail.keytype"
	textDetailFingerprint     kittui.TextID = "detail.fingerprint"
	textDetailDir             kittui.TextID = "detail.dir"
	textHeaderWorking         kittui.TextID = "header.working"
	textRepoNone              kittui.TextID = "repo.none"
	textDetailStacks          kittui.TextID = "detail.stacks"
	textDetailPollError       kittui.TextID = "detail.pollerror"
	textRepoRebind            kittui.TextID = "repo.rebind"
	textKeyNone               kittui.TextID = "key.none"
	textKeyCopy               kittui.TextID = "key.copy"
	textDetailDays            kittui.TextID = "detail.days"
	textWeekdayMon            kittui.TextID = "weekday.mon"
	textWeekdayTue            kittui.TextID = "weekday.tue"
	textWeekdayWed            kittui.TextID = "weekday.wed"
	textWeekdayThu            kittui.TextID = "weekday.thu"
	textWeekdayFri            kittui.TextID = "weekday.fri"
	textWeekdaySat            kittui.TextID = "weekday.sat"
	textWeekdaySun            kittui.TextID = "weekday.sun"
	textDetailStart           kittui.TextID = "detail.start"
	textDetailDuration        kittui.TextID = "detail.duration"
	textDetailZone            kittui.TextID = "detail.zone"
	textDetailNext            kittui.TextID = "detail.next"
	textDetailLastRun         kittui.TextID = "detail.lastrunat"
	textTitleBind             kittui.TextID = "title.bind"
	textTitleCred             kittui.TextID = "title.cred"
	textTitleWindow           kittui.TextID = "title.window"
	textTitleNotify           kittui.TextID = "title.notify"
	textTitleNotifyDetail     kittui.TextID = "title.notifydetail"
	textFieldURL              kittui.TextID = "field.url"
	textHintURL               kittui.TextID = "hint.url"
	textFieldDir              kittui.TextID = "field.dir"
	textHintDir               kittui.TextID = "hint.dir"
	textFieldCredKind         kittui.TextID = "field.credkind"
	textHintCredKind          kittui.TextID = "hint.credkind"
	textKindRegistry          kittui.TextID = "kind.registry"
	textKindGitHub            kittui.TextID = "kind.github"
	textFieldRegistryHost     kittui.TextID = "field.registryhost"
	textHintRegistryHost      kittui.TextID = "hint.registryhost"
	textFieldRegistryUser     kittui.TextID = "field.registryuser"
	textHintRegistryUser      kittui.TextID = "hint.registryuser"
	textFieldToken            kittui.TextID = "field.token"
	textHintToken             kittui.TextID = "hint.token"
	textFieldDays             kittui.TextID = "field.days"
	textHintDays              kittui.TextID = "hint.days"
	textFieldStart            kittui.TextID = "field.start"
	textHintStart             kittui.TextID = "hint.start"
	textFieldDuration         kittui.TextID = "field.duration"
	textHintDuration          kittui.TextID = "hint.duration"
	textFieldZone             kittui.TextID = "field.zone"
	textHintZone              kittui.TextID = "hint.zone"
	textEventSucceeded        kittui.TextID = "event.succeeded"
	textEventFailed           kittui.TextID = "event.failed"
	textEventRolledBack       kittui.TextID = "event.rolledback"
	textEventRollbackFailed   kittui.TextID = "event.rollbackfailed"
	textEventContainerFailed  kittui.TextID = "event.containerfailed"
	textEventRecovered        kittui.TextID = "event.recovered"
	textEventUpdateAvailable  kittui.TextID = "event.updateavailable"
	textFieldName             kittui.TextID = "field.name"
	textHintName              kittui.TextID = "hint.name"
	textFieldType             kittui.TextID = "field.type"
	textHintType              kittui.TextID = "hint.type"
	textFieldEvents           kittui.TextID = "field.events"
	textHintEvents            kittui.TextID = "hint.events"
	textTypeWebhook           kittui.TextID = "type.webhook"
	textTypeSlack             kittui.TextID = "type.slack"
	textTypeTelegram          kittui.TextID = "type.telegram"
	textTypeSMTP              kittui.TextID = "type.smtp"
	textFieldWebhookURL       kittui.TextID = "field.webhookurl"
	textHintWebhookURL        kittui.TextID = "hint.webhookurl"
	textFieldSigningKey       kittui.TextID = "field.signingkey"
	textHintSigningKey        kittui.TextID = "hint.signingkey"
	textFieldSlackURL         kittui.TextID = "field.slackurl"
	textHintSlackURL          kittui.TextID = "hint.slackurl"
	textFieldBotToken         kittui.TextID = "field.bottoken"
	textHintBotToken          kittui.TextID = "hint.bottoken"
	textFieldChatID           kittui.TextID = "field.chatid"
	textHintChatID            kittui.TextID = "hint.chatid"
	textFieldBaseURL          kittui.TextID = "field.baseurl"
	textHintBaseURL           kittui.TextID = "hint.baseurl"
	textFieldHost             kittui.TextID = "field.host"
	textHintHost              kittui.TextID = "hint.host"
	textFieldPort             kittui.TextID = "field.port"
	textHintPort              kittui.TextID = "hint.port"
	textFieldSecurity         kittui.TextID = "field.security"
	textHintSecurity          kittui.TextID = "hint.security"
	textSecStartTLS           kittui.TextID = "sec.starttls"
	textSecTLS                kittui.TextID = "sec.tls"
	textFieldUser             kittui.TextID = "field.user"
	textHintUser              kittui.TextID = "hint.user"
	textFieldPassword         kittui.TextID = "field.password"
	textHintPassword          kittui.TextID = "hint.password"
	textFieldFrom             kittui.TextID = "field.from"
	textHintFrom              kittui.TextID = "hint.from"
	textFieldTo               kittui.TextID = "field.to"
	textHintTo                kittui.TextID = "hint.to"
	textProblemURL            kittui.TextID = "problem.url"
	textProblemDir            kittui.TextID = "problem.dir"
	textProblemHost           kittui.TextID = "problem.host"
	textProblemUser           kittui.TextID = "problem.user"
	textProblemSecret         kittui.TextID = "problem.secret"
	textProblemNoChange       kittui.TextID = "problem.nochange"
	textProblemChannelName    kittui.TextID = "problem.channelname"
	textProblemRequired       kittui.TextID = "problem.required"
	textProblemPort           kittui.TextID = "problem.port"
	textProblemPasswordNoUser kittui.TextID = "problem.passwordnouser"
	textAskCredSet            kittui.TextID = "ask.credset"
	textWhatCredSet           kittui.TextID = "what.credset"
	textAskWindow             kittui.TextID = "ask.window"
	textWhatWindow            kittui.TextID = "what.window"
	textAskNotifyAdd          kittui.TextID = "ask.notifyadd"
	textWhatNotifyAdd         kittui.TextID = "what.notifyadd"
)

func settingsEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textAreaSettings:          "Settings",
		textKeyNext:               "next field",
		textKeyPrev:               "previous field",
		textKeySave:               "save",
		textKeyBind:               "bind",
		textKeyFetch:              "fetch now",
		textKeyGenerate:           "generate key",
		textKeyAdd:                "add",
		textKeyRemove:             "remove",
		textKeyEdit:               "edit",
		textKeyTest:               "send test",
		textKeyOpen:               "open",
		textKeyCancelForm:         "discard",
		textColSetting:            "setting",
		textColAbout:              "about",
		textColName:               "name",
		textColModified:           "stored",
		textColType:               "type",
		textColEvents:             "events",
		textColTarget:             "target",
		textPageRepo:              "Stacks repository",
		textPageKey:               "Deploy key",
		textPageCreds:             "Credentials",
		textPageWindow:            "Maintenance window",
		textPageNotify:            "Notifications",
		textAboutRepo:             "bind the repository, confirm its host key, fetch",
		textAboutKey:              "show or generate the key for the git platform",
		textAboutCreds:            "registry logins and the GitHub token",
		textAboutWindow:           "when updates run",
		textAboutNotify:           "webhook, Slack, Telegram and e-mail channels",
		textCredsEmpty:            "No credentials stored; qdeploy works anonymously. {add} adds one.",
		textNotifyEmpty:           "No notification channels. {add} adds one.",
		textEventsAll:             "all",
		textSettingDoneTitle:      "Done",
		textDoneBound:             "The stacks repository is bound.",
		textDetailHead:            "commit: %s",
		textDetailRemote:          "remote commit: %s",
		textDetailCloned:          "cloned now: %s",
		textDoneFetchedCurrent:    "Fetched; the working copy is up to date.",
		textDoneFetchedNew:        "Fetched; the remote has newer commits.",
		textDoneGenerated:         "The deploy key is generated. Its fingerprint:",
		textDoneWindow:            "The maintenance window is changed.",
		textDoneAdded:             "The channel %s is added.",
		textDoneTested:            "The test message through %s was delivered.",
		textDoneStored:            "The credential %s is stored.",
		textDoneDeleted:           "The credential %s is deleted.",
		textDoneRemoved:           "The channel %s is removed.",
		textFormRetry:             "Correct the form and save again, or discard it.",
		textFormFix:               "Correct the field.",
		textYes:                   "yes",
		textNo:                    "no",
		textKeyExists:             "A deploy key exists already; replacing it is only possible with 'qdeploy key generate --replace'.",
		textChooseOne:             "Choose an entry first.",
		textAskFetch:              "Fetch the stacks repository now?",
		textWhatFetch:             "Updates the remote-tracking branch; the working copy stays as it is.",
		textAskGenerate:           "Generate the deploy key?",
		textWhatGenerate:          "The service creates an Ed25519 key. Its private part never leaves the service.",
		textAskCredDelete:         "Delete the credential %s?",
		textWhatCredDelete:        "Without it qdeploy works anonymously for this service. The value cannot be recovered.",
		textAskNotifyRemove:       "Remove the channel %s?",
		textWhatNotifyRemove:      "The channel and its stored secrets are deleted.",
		textAskNotifyTest:         "Send a test message through %s?",
		textWhatNotifyTest:        "One short message is sent to the channel, without retries.",
		textAskFolderCreate:       "Create the stack folder %s?",
		textWhatFolderCreate:      "The folder is created with owner qdeploy:qdeploy and mode 0755, the service is granted write access to it in a systemd drop-in and restarts once.",
		textProblemDirRequired:    "The stack folder is required: enter an absolute path.",
		textAskBind:               "Bind %s and trust this host key?",
		textWhatBind:              "Compare the fingerprint with the git platform first. The key is stored only now, and the repository is cloned.",
		textWhatBindPinned:        "The service already trusts exactly this host key. The repository is cloned.",
		textDetailHost:            "host: %s",
		textDetailKeyType:         "key type: %s",
		textDetailFingerprint:     "fingerprint: %s",
		textDetailDir:             "directory: %s",
		textHeaderWorking:         "working",
		textRepoNone:              "No stacks repository is bound. Press {bind} to bind one.",
		textDetailStacks:          "stacks: %d",
		textDetailPollError:       "poll failed: %s",
		textRepoRebind:            "{bind} binds a repository, {fetch} fetches new commits now.",
		textKeyNone:               "No deploy key yet. Press {generate} to create one.",
		textKeyCopy:               "Add this public key as a deploy key on the git platform:",
		textDetailDays:            "days: %s",
		textWeekdayMon:            "Mon",
		textWeekdayTue:            "Tue",
		textWeekdayWed:            "Wed",
		textWeekdayThu:            "Thu",
		textWeekdayFri:            "Fri",
		textWeekdaySat:            "Sat",
		textWeekdaySun:            "Sun",
		textDetailStart:           "start: %s",
		textDetailDuration:        "duration: %s",
		textDetailZone:            "time zone: %s",
		textDetailNext:            "next opening: %s",
		textDetailLastRun:         "last work began: %s",
		textTitleBind:             "Bind the stacks repository",
		textTitleCred:             "Set a credential",
		textTitleWindow:           "Change the maintenance window",
		textTitleNotify:           "Add a notification channel",
		textTitleNotifyDetail:     "Settings of the channel",
		textFieldURL:              "SSH URL",
		textHintURL:               "user@host:path or ssh://[user@]host[:port]/path",
		textFieldDir:              "Directory",
		textHintDir:               "absolute path of the working copy",
		textFieldCredKind:         "Credential",
		textHintCredKind:          "left and right choose",
		textKindRegistry:          "registry login",
		textKindGitHub:            "GitHub token for changelogs",
		textFieldRegistryHost:     "Registry host",
		textHintRegistryHost:      "registry login only; with port as host:port, Docker Hub as docker.io",
		textFieldRegistryUser:     "Registry user",
		textHintRegistryUser:      "registry login only; no colon",
		textFieldToken:            "Token",
		textHintToken:             "hidden; the registry token, or the GitHub token",
		textFieldDays:             "Days",
		textHintDays:              "daily, or weekdays such as mon,thu",
		textFieldStart:            "Start",
		textHintStart:             "HH:MM on the 24-hour clock",
		textFieldDuration:         "Duration",
		textHintDuration:          "1m to 12h, such as 2h or 1h30m",
		textFieldZone:             "Time zone",
		textHintZone:              "local, or an IANA name such as Europe/Berlin",
		textEventSucceeded:        "succeeded",
		textEventFailed:           "failed",
		textEventRolledBack:       "rolled-back",
		textEventRollbackFailed:   "rollback-failed",
		textEventContainerFailed:  "container-failed",
		textEventRecovered:        "recovered",
		textEventUpdateAvailable:  "update-available",
		textFieldName:             "Name",
		textHintName:              "lowercase letters, digits and -, starting with a letter, up to 32",
		textFieldType:             "Type",
		textHintType:              "left and right choose",
		textFieldEvents:           "Events",
		textHintEvents:            "space ticks; none ticked means all",
		textTypeWebhook:           "webhook",
		textTypeSlack:             "slack",
		textTypeTelegram:          "telegram",
		textTypeSMTP:              "smtp",
		textFieldWebhookURL:       "Webhook URL",
		textHintWebhookURL:        "hidden; an https address",
		textFieldSigningKey:       "Signing key",
		textHintSigningKey:        "hidden; signs every message",
		textFieldSlackURL:         "Incoming webhook URL",
		textHintSlackURL:          "hidden; from the Slack app",
		textFieldBotToken:         "Bot token",
		textHintBotToken:          "hidden; from BotFather",
		textFieldChatID:           "Chat ID",
		textHintChatID:            "the chat the bot writes to",
		textFieldBaseURL:          "Bot API address",
		textHintBaseURL:           "optional; a Bot API server instead of the public one",
		textFieldHost:             "SMTP host",
		textHintHost:              "the mail server",
		textFieldPort:             "Port",
		textHintPort:              "optional; 587 with starttls, 465 with tls",
		textFieldSecurity:         "Security",
		textHintSecurity:          "left and right choose",
		textSecStartTLS:           "starttls",
		textSecTLS:                "tls",
		textFieldUser:             "User",
		textHintUser:              "optional; the login name",
		textFieldPassword:         "Password",
		textHintPassword:          "hidden; only with a user",
		textFieldFrom:             "From",
		textHintFrom:              "the sender address",
		textFieldTo:               "To",
		textHintTo:                "recipient addresses, comma separated",
		textProblemURL:            "The SSH address is required and holds no blanks.",
		textProblemDir:            "The directory must be an absolute, clean path.",
		textProblemHost:           "This is no registry host.",
		textProblemUser:           "The user is required and holds no colon.",
		textProblemSecret:         "A value of 4 to 8192 characters without control characters is required.",
		textProblemNoChange:       "Nothing differs from the current window.",
		textProblemChannelName:    "A name has 1 to 32 characters: lowercase letters, digits and -, starting with a letter.",
		textProblemRequired:       "This field is required.",
		textProblemPort:           "The port is a number from 1 to 65535.",
		textProblemPasswordNoUser: "A password needs a user.",
		textAskCredSet:            "Store the credential %s?",
		textWhatCredSet:           "The service stores the value and never shows it again. A credential of that name is replaced.",
		textAskWindow:             "Change the maintenance window?",
		textWhatWindow:            "The running service follows at once.",
		textAskNotifyAdd:          "Add the channel %s (%s)?",
		textWhatNotifyAdd:         "The service stores the secret parts and never shows them again.",
	}
}

func settingsGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textAreaSettings:          "Einstellungen",
		textKeyNext:               "nächstes Feld",
		textKeyPrev:               "voriges Feld",
		textKeySave:               "speichern",
		textKeyBind:               "binden",
		textKeyFetch:              "jetzt abrufen",
		textKeyGenerate:           "Schlüssel erzeugen",
		textKeyAdd:                "hinzufügen",
		textKeyRemove:             "entfernen",
		textKeyEdit:               "ändern",
		textKeyTest:               "Test senden",
		textKeyOpen:               "öffnen",
		textKeyCancelForm:         "verwerfen",
		textColSetting:            "Einstellung",
		textColAbout:              "Inhalt",
		textColName:               "Name",
		textColModified:           "gespeichert",
		textColType:               "Typ",
		textColEvents:             "Ereignisse",
		textColTarget:             "Ziel",
		textPageRepo:              "Stacks-Repository",
		textPageKey:               "Deploy-Schlüssel",
		textPageCreds:             "Zugangsdaten",
		textPageWindow:            "Wartungsfenster",
		textPageNotify:            "Benachrichtigungen",
		textAboutRepo:             "Repository binden, Hostschlüssel bestätigen, abrufen",
		textAboutKey:              "Schlüssel für die Git-Plattform zeigen oder erzeugen",
		textAboutCreds:            "Registry-Logins und GitHub-Token",
		textAboutWindow:           "wann Updates laufen",
		textAboutNotify:           "Kanäle für Webhook, Slack, Telegram und E-Mail",
		textCredsEmpty:            "Keine Zugangsdaten gespeichert; qdeploy arbeitet anonym. {add} legt eine an.",
		textNotifyEmpty:           "Keine Benachrichtigungskanäle. {add} legt einen an.",
		textEventsAll:             "alle",
		textSettingDoneTitle:      "Erledigt",
		textDoneBound:             "Das Stacks-Repository ist gebunden.",
		textDetailHead:            "Commit: %s",
		textDetailRemote:          "Remote-Commit: %s",
		textDetailCloned:          "jetzt geklont: %s",
		textDoneFetchedCurrent:    "Abgerufen; die Arbeitskopie ist aktuell.",
		textDoneFetchedNew:        "Abgerufen; das Remote hat neuere Commits.",
		textDoneGenerated:         "Der Deploy-Schlüssel ist erzeugt. Sein Fingerabdruck:",
		textDoneWindow:            "Das Wartungsfenster ist geändert.",
		textDoneAdded:             "Der Kanal %s ist angelegt.",
		textDoneTested:            "Die Testnachricht über %s wurde zugestellt.",
		textDoneStored:            "Die Zugangsdaten %s sind gespeichert.",
		textDoneDeleted:           "Die Zugangsdaten %s sind gelöscht.",
		textDoneRemoved:           "Der Kanal %s ist entfernt.",
		textFormRetry:             "Korrigiere das Formular und speichere erneut oder verwirf es.",
		textFormFix:               "Korrigiere das Feld.",
		textYes:                   "ja",
		textNo:                    "nein",
		textKeyExists:             "Ein Deploy-Schlüssel existiert bereits; ersetzen geht nur mit 'qdeploy key generate --replace'.",
		textChooseOne:             "Wähle zuerst einen Eintrag.",
		textAskFetch:              "Das Stacks-Repository jetzt abrufen?",
		textWhatFetch:             "Aktualisiert den Remote-Zweig; die Arbeitskopie bleibt unverändert.",
		textAskGenerate:           "Den Deploy-Schlüssel erzeugen?",
		textWhatGenerate:          "Der Dienst erzeugt einen Ed25519-Schlüssel. Der private Teil verlässt den Dienst nie.",
		textAskCredDelete:         "Die Zugangsdaten %s löschen?",
		textWhatCredDelete:        "Ohne sie arbeitet qdeploy bei diesem Dienst anonym. Der Wert ist nicht wiederherstellbar.",
		textAskNotifyRemove:       "Den Kanal %s entfernen?",
		textWhatNotifyRemove:      "Der Kanal und seine gespeicherten Geheimnisse werden gelöscht.",
		textAskNotifyTest:         "Eine Testnachricht über %s senden?",
		textWhatNotifyTest:        "Eine kurze Nachricht geht einmal an den Kanal, ohne Wiederholung.",
		textAskFolderCreate:       "Stack-Ordner %s anlegen?",
		textWhatFolderCreate:      "Der Ordner wird mit Eigentümer qdeploy:qdeploy und Modus 0755 angelegt, der Dienst erhält dafür in einem systemd-Drop-in Schreibzugriff und startet einmal neu.",
		textProblemDirRequired:    "Der Stack-Ordner ist erforderlich: Gib einen absoluten Pfad ein.",
		textAskBind:               "%s binden und diesem Hostschlüssel vertrauen?",
		textWhatBind:              "Vergleiche den Fingerabdruck zuerst mit der Git-Plattform. Der Schlüssel wird erst jetzt gespeichert und das Repository geklont.",
		textWhatBindPinned:        "Der Dienst vertraut genau diesem Hostschlüssel bereits. Das Repository wird geklont.",
		textDetailHost:            "Host: %s",
		textDetailKeyType:         "Schlüsseltyp: %s",
		textDetailFingerprint:     "Fingerabdruck: %s",
		textDetailDir:             "Verzeichnis: %s",
		textHeaderWorking:         "arbeitet",
		textRepoNone:              "Es ist kein Stacks-Repository gebunden. {bind} bindet eines.",
		textDetailStacks:          "Stacks: %d",
		textDetailPollError:       "Abruf fehlgeschlagen: %s",
		textRepoRebind:            "{bind} bindet ein Repository, {fetch} ruft neue Commits jetzt ab.",
		textKeyNone:               "Noch kein Deploy-Schlüssel. {generate} erzeugt einen.",
		textKeyCopy:               "Trage diesen öffentlichen Schlüssel als Deploy-Key bei der Git-Plattform ein:",
		textDetailDays:            "Tage: %s",
		textWeekdayMon:            "Mo",
		textWeekdayTue:            "Di",
		textWeekdayWed:            "Mi",
		textWeekdayThu:            "Do",
		textWeekdayFri:            "Fr",
		textWeekdaySat:            "Sa",
		textWeekdaySun:            "So",
		textDetailStart:           "Beginn: %s",
		textDetailDuration:        "Dauer: %s",
		textDetailZone:            "Zeitzone: %s",
		textDetailNext:            "nächste Öffnung: %s",
		textDetailLastRun:         "letzte Arbeit begann: %s",
		textTitleBind:             "Stacks-Repository binden",
		textTitleCred:             "Zugangsdaten setzen",
		textTitleWindow:           "Wartungsfenster ändern",
		textTitleNotify:           "Benachrichtigungskanal anlegen",
		textTitleNotifyDetail:     "Einstellungen des Kanals",
		textFieldURL:              "SSH-URL",
		textHintURL:               "user@host:pfad oder ssh://[user@]host[:port]/pfad",
		textFieldDir:              "Verzeichnis",
		textHintDir:               "absoluter Pfad der Arbeitskopie",
		textFieldCredKind:         "Zugangsdaten",
		textHintCredKind:          "links und rechts wählen",
		textKindRegistry:          "Registry-Login",
		textKindGitHub:            "GitHub-Token für Changelogs",
		textFieldRegistryHost:     "Registry-Host",
		textHintRegistryHost:      "nur beim Registry-Login; mit Port als host:port, Docker Hub als docker.io",
		textFieldRegistryUser:     "Registry-Benutzer",
		textHintRegistryUser:      "nur beim Registry-Login; ohne Doppelpunkt",
		textFieldToken:            "Token",
		textHintToken:             "verborgen; das Registry-Token oder das GitHub-Token",
		textFieldDays:             "Tage",
		textHintDays:              "daily oder Wochentage wie mon,thu",
		textFieldStart:            "Beginn",
		textHintStart:             "HH:MM im 24-Stunden-Format",
		textFieldDuration:         "Dauer",
		textHintDuration:          "1m bis 12h, etwa 2h oder 1h30m",
		textFieldZone:             "Zeitzone",
		textHintZone:              "local oder ein IANA-Name wie Europe/Berlin",
		textEventSucceeded:        "succeeded",
		textEventFailed:           "failed",
		textEventRolledBack:       "rolled-back",
		textEventRollbackFailed:   "rollback-failed",
		textEventContainerFailed:  "container-failed",
		textEventRecovered:        "recovered",
		textEventUpdateAvailable:  "update-available",
		textFieldName:             "Name",
		textHintName:              "Kleinbuchstaben, Ziffern und -, mit einem Buchstaben beginnend, bis 32",
		textFieldType:             "Typ",
		textHintType:              "links und rechts wählen",
		textFieldEvents:           "Ereignisse",
		textHintEvents:            "Leertaste hakt an; nichts angehakt heißt alle",
		textTypeWebhook:           "webhook",
		textTypeSlack:             "slack",
		textTypeTelegram:          "telegram",
		textTypeSMTP:              "smtp",
		textFieldWebhookURL:       "Webhook-URL",
		textHintWebhookURL:        "verborgen; eine https-Adresse",
		textFieldSigningKey:       "Signaturschlüssel",
		textHintSigningKey:        "verborgen; signiert jede Nachricht",
		textFieldSlackURL:         "Incoming-Webhook-URL",
		textHintSlackURL:          "verborgen; aus der Slack-App",
		textFieldBotToken:         "Bot-Token",
		textHintBotToken:          "verborgen; von BotFather",
		textFieldChatID:           "Chat-ID",
		textHintChatID:            "der Chat, in den der Bot schreibt",
		textFieldBaseURL:          "Bot-API-Adresse",
		textHintBaseURL:           "optional; ein Bot-API-Server statt des öffentlichen",
		textFieldHost:             "SMTP-Host",
		textHintHost:              "der Mailserver",
		textFieldPort:             "Port",
		textHintPort:              "optional; 587 bei starttls, 465 bei tls",
		textFieldSecurity:         "Verschlüsselung",
		textHintSecurity:          "links und rechts wählen",
		textSecStartTLS:           "starttls",
		textSecTLS:                "tls",
		textFieldUser:             "Benutzer",
		textHintUser:              "optional; der Anmeldename",
		textFieldPassword:         "Passwort",
		textHintPassword:          "verborgen; nur mit Benutzer",
		textFieldFrom:             "Absender",
		textHintFrom:              "die Absenderadresse",
		textFieldTo:               "Empfänger",
		textHintTo:                "Empfängeradressen, durch Komma getrennt",
		textProblemURL:            "Die SSH-Adresse ist nötig und enthält keine Leerzeichen.",
		textProblemDir:            "Das Verzeichnis muss ein absoluter, bereinigter Pfad sein.",
		textProblemHost:           "Das ist kein Registry-Host.",
		textProblemUser:           "Der Benutzer ist nötig und enthält keinen Doppelpunkt.",
		textProblemSecret:         "Nötig ist ein Wert von 4 bis 8192 Zeichen ohne Steuerzeichen.",
		textProblemNoChange:       "Nichts weicht vom jetzigen Fenster ab.",
		textProblemChannelName:    "Ein Name hat 1 bis 32 Zeichen: Kleinbuchstaben, Ziffern und -, beginnend mit einem Buchstaben.",
		textProblemRequired:       "Dieses Feld ist nötig.",
		textProblemPort:           "Der Port ist eine Zahl von 1 bis 65535.",
		textProblemPasswordNoUser: "Ein Passwort braucht einen Benutzer.",
		textAskCredSet:            "Die Zugangsdaten %s speichern?",
		textWhatCredSet:           "Der Dienst speichert den Wert und zeigt ihn nie wieder. Zugangsdaten gleichen Namens werden ersetzt.",
		textAskWindow:             "Das Wartungsfenster ändern?",
		textWhatWindow:            "Der laufende Dienst folgt sofort.",
		textAskNotifyAdd:          "Den Kanal %s (%s) anlegen?",
		textWhatNotifyAdd:         "Der Dienst speichert die geheimen Teile und zeigt sie nie wieder.",
	}
}
