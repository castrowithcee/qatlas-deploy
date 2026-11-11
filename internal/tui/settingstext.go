package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The texts of the Settings area. Technical words of the service (types, events, commands) stay English in
// every language.
const (
	textAreaSettings          kittui.TextID = "area.settings"
	textKeyNext               kittui.TextID = "key.next"
	textKeyPrev               kittui.TextID = "key.prev"
	textKeySave               kittui.TextID = "key.save"
	textKeyFetch              kittui.TextID = "key.fetch"
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
	textPageGeneral           kittui.TextID = "page.general"
	textAboutGeneral          kittui.TextID = "about.general"
	textFieldTheme            kittui.TextID = "field.theme"
	textHintTheme             kittui.TextID = "hint.theme"
	textFieldStartArea        kittui.TextID = "field.startarea"
	textHintStartArea         kittui.TextID = "hint.startarea"
	textFieldLanguage         kittui.TextID = "field.language"
	textHintLanguage          kittui.TextID = "hint.language"
	textFieldMouse            kittui.TextID = "field.mouse"
	textHintMouse             kittui.TextID = "hint.mouse"
	textLangEnglish           kittui.TextID = "lang.english"
	textLangGerman            kittui.TextID = "lang.german"
	textLangSystem            kittui.TextID = "lang.system"
	textMouseOn               kittui.TextID = "mouse.on"
	textMouseOff              kittui.TextID = "mouse.off"
	textGeneralSaved          kittui.TextID = "general.saved"
	textGeneralSavedStart     kittui.TextID = "general.saved.start"
	textGeneralCannotSave     kittui.TextID = "general.cannotsave"
	textGeneralSaveFailed     kittui.TextID = "general.savefailed"
	textPageKey               kittui.TextID = "page.key"
	textPageCreds             kittui.TextID = "page.creds"
	textPageWindow            kittui.TextID = "page.window"
	textPageNotify            kittui.TextID = "page.notify"
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
	textChooseOne             kittui.TextID = "choose.one"
	textAskFetch              kittui.TextID = "ask.fetch"
	textWhatFetch             kittui.TextID = "what.fetch"
	textAskCredDelete         kittui.TextID = "ask.creddelete"
	textWhatCredDelete        kittui.TextID = "what.creddelete"
	textAskNotifyRemove       kittui.TextID = "ask.notifyremove"
	textWhatNotifyRemove      kittui.TextID = "what.notifyremove"
	textAskNotifyTest         kittui.TextID = "ask.notifytest"
	textWhatNotifyTest        kittui.TextID = "what.notifytest"
	textProblemDirRequired    kittui.TextID = "problem.dirrequired"
	textDetailHost            kittui.TextID = "detail.host"
	textDetailKeyType         kittui.TextID = "detail.keytype"
	textDetailFingerprint     kittui.TextID = "detail.fingerprint"
	textDetailDir             kittui.TextID = "detail.dir"
	textHeaderWorking         kittui.TextID = "header.working"
	textDetailPollError       kittui.TextID = "detail.pollerror"
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
	textTitleCred             kittui.TextID = "title.cred"
	textTitleWindow           kittui.TextID = "title.window"
	textTitleNotify           kittui.TextID = "title.notify"
	textTitleNotifyDetail     kittui.TextID = "title.notifydetail"
	textFieldURL              kittui.TextID = "field.url"
	textHintURL               kittui.TextID = "hint.url"
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
	textEventRecoveryFailed   kittui.TextID = "event.recoveryfailed"
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
	textTitleDiscard          kittui.TextID = "title.discard"
	textAskDiscard            kittui.TextID = "ask.discard"
	textWhatDiscard           kittui.TextID = "what.discard"
	textPageFolders           kittui.TextID = "page.folders"
	textAboutFolders          kittui.TextID = "about.folders"
	textColPath               kittui.TextID = "col.path"
	textColKind               kittui.TextID = "col.kind"
	textColCommit             kittui.TextID = "col.commit"
	textColPolled             kittui.TextID = "col.polled"
	textFolderKindRemote      kittui.TextID = "folder.remote"
	textFolderKindLocal       kittui.TextID = "folder.local"
	textFolderOK              kittui.TextID = "folder.ok"
	textFolderFailed          kittui.TextID = "folder.failed"
	textFoldersEmpty          kittui.TextID = "folders.empty"
	textDetailURL             kittui.TextID = "detail.url"
	textDetailLastPoll        kittui.TextID = "detail.lastpoll"
	textDetailNote            kittui.TextID = "detail.note"
	textKeyRelease            kittui.TextID = "key.release"
	textKeyDeployKey          kittui.TextID = "key.deploykey"
	textAnswerContinue        kittui.TextID = "answer.continue"
	textTitleFolderAdd        kittui.TextID = "title.folderadd"
	textFieldFolderPath       kittui.TextID = "field.folderpath"
	textHintFolderPath        kittui.TextID = "hint.folderpath"
	textTitleFolderURL        kittui.TextID = "title.folderurl"
	textFoldersNone           kittui.TextID = "folders.none"
	textDetailCase            kittui.TextID = "detail.case"
	textCaseClone             kittui.TextID = "case.clone"
	textCaseAdopt             kittui.TextID = "case.adopt"
	textCaseLocal             kittui.TextID = "case.local"
	textCaseFiles             kittui.TextID = "case.files"
	textCaseReject            kittui.TextID = "case.reject"
	textDetailReason          kittui.TextID = "detail.reason"
	textDetailFix             kittui.TextID = "detail.fix"
	textReasonReadOnly        kittui.TextID = "reason.readonly"
	textFixReadOnly           kittui.TextID = "fix.readonly"
	textDetailMissing         kittui.TextID = "detail.missing"
	textDetailOwner           kittui.TextID = "detail.owner"
	textDetailServiceWrites   kittui.TextID = "detail.servicewrites"
	textDetailSetsPerms       kittui.TextID = "detail.setsperms"
	textDetailGitWritable     kittui.TextID = "detail.gitwritable"
	textHeadFiles             kittui.TextID = "head.files"
	textHeadExcluded          kittui.TextID = "head.excluded"
	textExcludedNone          kittui.TextID = "excluded.none"
	textExcludedEntry         kittui.TextID = "excluded.entry"
	textExcludedStack         kittui.TextID = "excluded.stack"
	textMoreEntries           kittui.TextID = "more.entries"
	textHeadStacks            kittui.TextID = "head.stacks"
	textStackFolder           kittui.TextID = "stack.folder"
	textStackWorks            kittui.TextID = "stack.works"
	textStackFails            kittui.TextID = "stack.fails"
	textStackFinding          kittui.TextID = "stack.finding"
	textStackConflict         kittui.TextID = "stack.conflict"
	textStacksError           kittui.TextID = "stacks.error"
	textContainersError       kittui.TextID = "containers.error"
	textHeadChanges           kittui.TextID = "head.changes"
	textChangeCreate          kittui.TextID = "change.create"
	textChangeClone           kittui.TextID = "change.clone"
	textChangeAdopt           kittui.TextID = "change.adopt"
	textChangeLocal           kittui.TextID = "change.local"
	textChangeFiles           kittui.TextID = "change.files"
	textChangeNoPerms         kittui.TextID = "change.noperms"
	textChangeDropIn          kittui.TextID = "change.dropin"
	textAskFolderRejected     kittui.TextID = "ask.folderrejected"
	textWhatNothing           kittui.TextID = "what.nothing"
	textAskFolderNoFiles      kittui.TextID = "ask.foldernofiles"
	textWhatFolderNoFiles     kittui.TextID = "what.foldernofiles"
	textAskFolderClone        kittui.TextID = "ask.folderclone"
	textWhatFolderClone       kittui.TextID = "what.folderclone"
	textAskFolderAdopt        kittui.TextID = "ask.folderadopt"
	textWhatFolderAdopt       kittui.TextID = "what.folderadopt"
	textAskFolderLocal        kittui.TextID = "ask.folderlocal"
	textWhatFolderApply       kittui.TextID = "what.folderapply"
	textAskFolderFiles        kittui.TextID = "ask.folderfiles"
	textWhatFolderFiles       kittui.TextID = "what.folderfiles"
	textAskHostKey            kittui.TextID = "ask.hostkey"
	textWhatHostKey           kittui.TextID = "what.hostkey"
	textWhatHostKeyPinned     kittui.TextID = "what.hostkeypinned"
	textAskFolderCloneFinal   kittui.TextID = "ask.folderclonefinal"
	textWhatFolderFinal       kittui.TextID = "what.folderfinal"
	textFolderNoKey           kittui.TextID = "folder.nokey"
	textFolderNoKeyNext       kittui.TextID = "folder.nokeynext"
	textFolderListChanged     kittui.TextID = "folder.listchanged"
	textFolderLocalFetch      kittui.TextID = "folder.localfetch"
	textFolderLocalKey        kittui.TextID = "folder.localkey"
	textFetchFailed           kittui.TextID = "fetch.failed"
	textAskRelease            kittui.TextID = "ask.release"
	textWhatRelease           kittui.TextID = "what.release"
	textDoneReleased          kittui.TextID = "done.released"
	textDoneFolderAdded       kittui.TextID = "done.folderadded"
	textDetailKind            kittui.TextID = "detail.kind"
	textDetailFiles           kittui.TextID = "detail.files"
	textKeptChanges           kittui.TextID = "kept.changes"
	textErrHost               kittui.TextID = "error.host"
)

func settingsEnglish() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textAreaSettings:          "Settings",
		textKeyNext:               "next field",
		textKeyPrev:               "previous field",
		textKeySave:               "save",
		textKeyFetch:              "fetch now",
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
		textPageKey:               "Deploy key",
		textPageCreds:             "Credentials",
		textPageWindow:            "Maintenance window",
		textPageNotify:            "Notifications",
		textPageGeneral:           "General",
		textAboutGeneral:          "theme, start area, language and mouse of this device",
		textFieldTheme:            "Theme",
		textHintTheme:             "the colors of the interface",
		textFieldStartArea:        "Start area",
		textHintStartArea:         "the area shown at start; applies from the next start",
		textFieldLanguage:         "Language",
		textHintLanguage:          "the language of the interface; system follows the device",
		textFieldMouse:            "Mouse",
		textHintMouse:             "with the mouse on, hold Shift to select text",
		textLangEnglish:           "English",
		textLangGerman:            "Deutsch",
		textLangSystem:            "System",
		textMouseOn:               "on",
		textMouseOff:              "off",
		textGeneralSaved:          "The settings are saved.",
		textGeneralSavedStart:     "The settings are saved. The start area applies from the next start.",
		textGeneralCannotSave:     "The settings cannot be saved here: this device has no place for them.",
		textGeneralSaveFailed:     "The settings could not be saved: %s",
		textAboutKey:              "store or generate the key for the git platform",
		textAboutCreds:            "registry logins and the GitHub token",
		textAboutWindow:           "when updates run",
		textAboutNotify:           "webhook, Slack, Telegram and e-mail channels",
		textCredsEmpty:            "No credentials stored; qdeploy works anonymously. {add} adds one.",
		textNotifyEmpty:           "No notification channels. {add} adds one.",
		textEventsAll:             "all",
		textSettingDoneTitle:      "Done",
		textDoneBound:             "The stack folder %s is bound.",
		textDetailHead:            "commit: %s",
		textDetailRemote:          "remote commit: %s",
		textDetailCloned:          "cloned now: %s",
		textDoneFetchedCurrent:    "Fetched %s; the working copy is up to date.",
		textDoneFetchedNew:        "Fetched %s; the remote has newer commits.",
		textDoneGenerated:         "The deploy key is generated.",
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
		textChooseOne:             "Choose an entry first.",
		textAskFetch:              "Fetch the stack folder %s now?",
		textWhatFetch:             "Updates the remote-tracking branch; the working copy stays as it is.",
		textAskCredDelete:         "Delete the credential %s?",
		textWhatCredDelete:        "Without it qdeploy works anonymously for this service. The value cannot be recovered.",
		textAskNotifyRemove:       "Remove the channel %s?",
		textWhatNotifyRemove:      "The channel and its stored secrets are deleted.",
		textAskNotifyTest:         "Send a test message through %s?",
		textWhatNotifyTest:        "One short message is sent to the channel, without retries.",
		textProblemDirRequired:    "The folder is required: enter an absolute path.",
		textDetailHost:            "host: %s",
		textDetailKeyType:         "key type: %s",
		textDetailFingerprint:     "fingerprint: %s",
		textDetailDir:             "folder: %s",
		textHeaderWorking:         "working",
		textDetailPollError:       "poll failed: %s",
		textKeyNone:               "No deploy key yet. Press {edit} to store or generate one.",
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
		textTitleCred:             "Set a credential",
		textTitleWindow:           "Change the maintenance window",
		textTitleNotify:           "Add a notification channel",
		textTitleNotifyDetail:     "Settings of the channel",
		textFieldURL:              "SSH URL",
		textHintURL:               "user@host:path or ssh://[user@]host[:port]/path",
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
		textEventRecoveryFailed:   "recovery-failed",
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
		textProblemDir:            "The path must be absolute and clean.",
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
		textTitleDiscard:          "Discard the changes",
		textAskDiscard:            "Discard the changes to this form?",
		textWhatDiscard:           "The changes you made here are lost.",
		textPageFolders:           "Stack folders",
		textAboutFolders:          "list, add, fetch and release the stack folders",
		textColPath:               "path",
		textColKind:               "kind",
		textColCommit:             "commit",
		textColPolled:             "last poll",
		textFolderKindRemote:      "remote",
		textFolderKindLocal:       "local",
		textFolderOK:              "ok",
		textFolderFailed:          "failed",
		textFoldersEmpty:          "No stack folders yet. {add} adds one.",
		textDetailURL:             "address: %s",
		textDetailLastPoll:        "last poll: %s",
		textDetailNote:            "note: %s",
		textKeyRelease:            "release",
		textKeyDeployKey:          "deploy key",
		textAnswerContinue:        "continue",
		textTitleFolderAdd:        "Add a stack folder",
		textFieldFolderPath:       "Folder",
		textHintFolderPath:        "absolute path; it is only inspected, nothing changes before the questions",
		textTitleFolderURL:        "Address of the repository to clone",
		textFoldersNone:           "This interface cannot reach the host side to add a stack folder.",
		textDetailCase:            "case: %s",
		textCaseClone:             "clone (the folder is missing or empty and gets a clone of a repository)",
		textCaseAdopt:             "adopt-clone (a clone with an origin remote, taken over as it is)",
		textCaseLocal:             "local (a repository without a remote, deployed as it is)",
		textCaseFiles:             "files (files without a repository; a first commit takes the listed files)",
		textCaseReject:            "reject (the folder cannot be taken over)",
		textDetailReason:          "reason: %s",
		textDetailFix:             "what to do: %s",
		textReasonReadOnly:        "the folder lies on a read-only file system",
		textFixReadOnly:           "Choose a folder on a writable file system, or remount the file system read-write.",
		textDetailMissing:         "the folder is missing; the nearest existing folder is %s",
		textDetailOwner:           "owner: %s, group: %s, mode: %s",
		textDetailServiceWrites:   "the service can write it: %s",
		textDetailSetsPerms:       "permissions are set: %s",
		textDetailGitWritable:     "the service can write .git: %s",
		textHeadFiles:             "Files of the first commit (%d)",
		textHeadExcluded:          "Left out (%d), recorded only in .git/info/exclude",
		textExcludedNone:          "Nothing is left out.",
		textExcludedEntry:         "%s: %s",
		textExcludedStack:         "%s: %s (stack %s)",
		textMoreEntries:           "... and %d more",
		textHeadStacks:            "Stacks (%d)",
		textStackFolder:           "%s (folder %s)",
		textStackWorks:            "works: %s",
		textStackFails:            "does not work: %s",
		textStackFinding:          "%s %s: %s",
		textStackConflict:         "conflict: the container %s of the project %s runs in the folder",
		textStacksError:           "the stacks cannot be read: %s",
		textContainersError:       "the containers were not checked: %s",
		textHeadChanges:           "Changes after yes",
		textChangeCreate:          "The folder is created with owner qdeploy:qdeploy and mode 0755.",
		textChangeClone:           "The host key is stored and the repository is cloned into the folder.",
		textChangeAdopt:           "The host key is stored and the clone is taken over with its address.",
		textChangeLocal:           "The repository is taken over as a local stack folder.",
		textChangeFiles:           "A git repository is started with branch main and a first commit of the listed files as qdeploy; no file in the folder is written. It is taken over as a local stack folder.",
		textChangeNoPerms:         "No permission changes: the service can write already.",
		textChangeDropIn:          "The folder is added to the systemd drop-in of the service, which restarts once.",
		textAskFolderRejected:     "%s cannot be taken over.",
		textWhatNothing:           "Nothing was changed.",
		textAskFolderNoFiles:      "%s has no file a first commit could take.",
		textWhatFolderNoFiles:     "Nothing was changed. Put the stack files into the folder first.",
		textAskFolderClone:        "Clone a repository into %s?",
		textWhatFolderClone:       "Next the SSH address is asked and the host key shown. Nothing changes before the last question.",
		textAskFolderAdopt:        "Take over the clone %s with the address %s?",
		textWhatFolderAdopt:       "Next the host key is shown and the deploy key of the folder is checked. Nothing changes before the last question.",
		textAskFolderLocal:        "Take over the repository %s as a local stack folder?",
		textWhatFolderApply:       "Yes makes the changes listed below.",
		textAskFolderFiles:        "Start a git repository in %s and take it over?",
		textWhatFolderFiles:       "Yes looks at the folder once more and makes the changes listed below only if its list is unchanged.",
		textAskHostKey:            "Trust this host key for %s?",
		textWhatHostKey:           "Compare the fingerprint with the git platform first. Nothing is stored yet; the key is passed on only after the last question.",
		textWhatHostKeyPinned:     "The service already trusts exactly this host key. Nothing changes before the last question.",
		textAskFolderCloneFinal:   "Clone %s into %s?",
		textWhatFolderFinal:       "Only now the host key is stored and the changes listed below are made.",
		textFolderNoKey:           "%s has no deploy key yet.",
		textFolderNoKeyNext:       "Store or generate it on this page and enter the public key on the git platform, then add the folder again. Nothing was changed.",
		textFolderListChanged:     "%s changed since its list was shown; nothing was changed. Add it again to review the new list.",
		textFolderLocalFetch:      "A local stack folder has no remote to fetch from.",
		textFolderLocalKey:        "A local stack folder has no deploy key.",
		textFetchFailed:           "the fetch of %s failed: %s",
		textAskRelease:            "Release the stack folder %s?",
		textWhatRelease:           "qdeploy no longer deploys from it. Nothing is deleted and no container is stopped; the permissions, the systemd drop-in entry and the deploy key stay.",
		textDoneReleased:          "The stack folder %s is released.",
		textDoneFolderAdded:       "The stack folder %s is added.",
		textDetailKind:            "kind: %s",
		textDetailFiles:           "files of the first commit: %d",
		textKeptChanges:           "%s; the group and the mode of %d paths stay changed",
		textErrHost:               "the host side failed: %s",
	}
}

func settingsGerman() map[kittui.TextID]string {
	return map[kittui.TextID]string{
		textAreaSettings:          "Einstellungen",
		textKeyNext:               "nächstes Feld",
		textKeyPrev:               "voriges Feld",
		textKeySave:               "speichern",
		textKeyFetch:              "jetzt abrufen",
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
		textPageKey:               "Deploy-Schlüssel",
		textPageCreds:             "Zugangsdaten",
		textPageWindow:            "Wartungsfenster",
		textPageNotify:            "Benachrichtigungen",
		textPageGeneral:           "Allgemein",
		textAboutGeneral:          "Design, Startbereich, Sprache und Maus dieses Geräts",
		textFieldTheme:            "Design",
		textHintTheme:             "die Farben der Oberfläche",
		textFieldStartArea:        "Startbereich",
		textHintStartArea:         "der Bereich beim Start; gilt ab dem nächsten Start",
		textFieldLanguage:         "Sprache",
		textHintLanguage:          "die Sprache der Oberfläche; System folgt dem Gerät",
		textFieldMouse:            "Maus",
		textHintMouse:             "bei eingeschalteter Maus Text mit gedrückter Umschalttaste markieren",
		textLangEnglish:           "English",
		textLangGerman:            "Deutsch",
		textLangSystem:            "System",
		textMouseOn:               "an",
		textMouseOff:              "aus",
		textGeneralSaved:          "Die Einstellungen sind gespeichert.",
		textGeneralSavedStart:     "Die Einstellungen sind gespeichert. Der Startbereich gilt ab dem nächsten Start.",
		textGeneralCannotSave:     "Die Einstellungen können hier nicht gespeichert werden: Dieses Gerät hat keinen Ort dafür.",
		textGeneralSaveFailed:     "Die Einstellungen konnten nicht gespeichert werden: %s",
		textAboutKey:              "Schlüssel für die Git-Plattform hinterlegen oder erzeugen",
		textAboutCreds:            "Registry-Logins und GitHub-Token",
		textAboutWindow:           "wann Updates laufen",
		textAboutNotify:           "Kanäle für Webhook, Slack, Telegram und E-Mail",
		textCredsEmpty:            "Keine Zugangsdaten gespeichert; qdeploy arbeitet anonym. {add} legt eine an.",
		textNotifyEmpty:           "Keine Benachrichtigungskanäle. {add} legt einen an.",
		textEventsAll:             "alle",
		textSettingDoneTitle:      "Erledigt",
		textDoneBound:             "Der Stack-Ordner %s ist gebunden.",
		textDetailHead:            "Commit: %s",
		textDetailRemote:          "Remote-Commit: %s",
		textDetailCloned:          "jetzt geklont: %s",
		textDoneFetchedCurrent:    "%s abgerufen; die Arbeitskopie ist aktuell.",
		textDoneFetchedNew:        "%s abgerufen; das Remote hat neuere Commits.",
		textDoneGenerated:         "Der Deploy-Schlüssel ist erzeugt.",
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
		textChooseOne:             "Wähle zuerst einen Eintrag.",
		textAskFetch:              "Den Stack-Ordner %s jetzt abrufen?",
		textWhatFetch:             "Aktualisiert den Remote-Zweig; die Arbeitskopie bleibt unverändert.",
		textAskCredDelete:         "Die Zugangsdaten %s löschen?",
		textWhatCredDelete:        "Ohne sie arbeitet qdeploy bei diesem Dienst anonym. Der Wert ist nicht wiederherstellbar.",
		textAskNotifyRemove:       "Den Kanal %s entfernen?",
		textWhatNotifyRemove:      "Der Kanal und seine gespeicherten Geheimnisse werden gelöscht.",
		textAskNotifyTest:         "Eine Testnachricht über %s senden?",
		textWhatNotifyTest:        "Eine kurze Nachricht geht einmal an den Kanal, ohne Wiederholung.",
		textProblemDirRequired:    "Der Ordner ist erforderlich: Gib einen absoluten Pfad ein.",
		textDetailHost:            "Host: %s",
		textDetailKeyType:         "Schlüsseltyp: %s",
		textDetailFingerprint:     "Fingerabdruck: %s",
		textDetailDir:             "Ordner: %s",
		textHeaderWorking:         "arbeitet",
		textDetailPollError:       "Abruf fehlgeschlagen: %s",
		textKeyNone:               "Noch kein Deploy-Schlüssel. {edit} hinterlegt oder erzeugt einen.",
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
		textTitleCred:             "Zugangsdaten setzen",
		textTitleWindow:           "Wartungsfenster ändern",
		textTitleNotify:           "Benachrichtigungskanal anlegen",
		textTitleNotifyDetail:     "Einstellungen des Kanals",
		textFieldURL:              "SSH-URL",
		textHintURL:               "user@host:pfad oder ssh://[user@]host[:port]/pfad",
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
		textEventRecoveryFailed:   "recovery-failed",
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
		textProblemDir:            "Der Pfad muss absolut und bereinigt sein.",
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
		textTitleDiscard:          "Änderungen verwerfen",
		textAskDiscard:            "Die Änderungen an diesem Formular verwerfen?",
		textWhatDiscard:           "Die Änderungen, die Sie hier gemacht haben, gehen verloren.",
		textPageFolders:           "Stack-Ordner",
		textAboutFolders:          "Stack-Ordner anzeigen, hinzufügen, abrufen und lösen",
		textColPath:               "Pfad",
		textColKind:               "Art",
		textColCommit:             "Commit",
		textColPolled:             "letzter Abruf",
		textFolderKindRemote:      "remote",
		textFolderKindLocal:       "lokal",
		textFolderOK:              "ok",
		textFolderFailed:          "Fehler",
		textFoldersEmpty:          "Noch keine Stack-Ordner. {add} fügt einen hinzu.",
		textDetailURL:             "Adresse: %s",
		textDetailLastPoll:        "letzter Abruf: %s",
		textDetailNote:            "Hinweis: %s",
		textKeyRelease:            "lösen",
		textKeyDeployKey:          "Deploy-Schlüssel",
		textAnswerContinue:        "weiter",
		textTitleFolderAdd:        "Stack-Ordner hinzufügen",
		textFieldFolderPath:       "Ordner",
		textHintFolderPath:        "absoluter Pfad; er wird nur geprüft, vor den Fragen ändert sich nichts",
		textTitleFolderURL:        "Adresse des zu klonenden Repositorys",
		textFoldersNone:           "Diese Oberfläche erreicht die Host-Seite zum Hinzufügen eines Stack-Ordners nicht.",
		textDetailCase:            "Fall: %s",
		textCaseClone:             "clone (der Ordner fehlt oder ist leer und bekommt einen Klon eines Repositorys)",
		textCaseAdopt:             "adopt-clone (ein Klon mit origin-Remote, der so übernommen wird)",
		textCaseLocal:             "local (ein Repository ohne Remote, das so ausgerollt wird)",
		textCaseFiles:             "files (Dateien ohne Repository; ein erster Commit nimmt die aufgeführten Dateien)",
		textCaseReject:            "reject (der Ordner kann nicht übernommen werden)",
		textDetailReason:          "Grund: %s",
		textDetailFix:             "Lösung: %s",
		textReasonReadOnly:        "der Ordner liegt auf einem schreibgeschützten Dateisystem",
		textFixReadOnly:           "Wähle einen Ordner auf einem beschreibbaren Dateisystem oder hänge das Dateisystem beschreibbar ein.",
		textDetailMissing:         "der Ordner fehlt; der nächste vorhandene Ordner ist %s",
		textDetailOwner:           "Eigentümer: %s, Gruppe: %s, Modus: %s",
		textDetailServiceWrites:   "der Dienst kann ihn schreiben: %s",
		textDetailSetsPerms:       "Rechte werden gesetzt: %s",
		textDetailGitWritable:     "der Dienst kann .git schreiben: %s",
		textHeadFiles:             "Dateien des ersten Commits (%d)",
		textHeadExcluded:          "Ausgelassen (%d), nur in .git/info/exclude vermerkt",
		textExcludedNone:          "Nichts wird ausgelassen.",
		textExcludedEntry:         "%s: %s",
		textExcludedStack:         "%s: %s (Stack %s)",
		textMoreEntries:           "... und %d weitere",
		textHeadStacks:            "Stacks (%d)",
		textStackFolder:           "%s (Ordner %s)",
		textStackWorks:            "funktioniert: %s",
		textStackFails:            "funktioniert nicht: %s",
		textStackFinding:          "%s %s: %s",
		textStackConflict:         "Konflikt: Der Container %s des Projekts %s läuft im Ordner",
		textStacksError:           "die Stacks sind nicht lesbar: %s",
		textContainersError:       "die Container wurden nicht geprüft: %s",
		textHeadChanges:           "Änderungen nach Ja",
		textChangeCreate:          "Der Ordner wird mit Eigentümer qdeploy:qdeploy und Modus 0755 angelegt.",
		textChangeClone:           "Der Hostschlüssel wird gespeichert und das Repository in den Ordner geklont.",
		textChangeAdopt:           "Der Hostschlüssel wird gespeichert und der Klon mit seiner Adresse übernommen.",
		textChangeLocal:           "Das Repository wird als lokaler Stack-Ordner übernommen.",
		textChangeFiles:           "Ein Git-Repository wird mit Branch main und einem ersten Commit der aufgeführten Dateien als qdeploy angelegt; keine Datei im Ordner wird geschrieben. Es wird als lokaler Stack-Ordner übernommen.",
		textChangeNoPerms:         "Keine Rechteänderung: Der Dienst kann bereits schreiben.",
		textChangeDropIn:          "Der Ordner wird in das systemd-Drop-in des Dienstes eingetragen, der einmal neu startet.",
		textAskFolderRejected:     "%s kann nicht übernommen werden.",
		textWhatNothing:           "Es wurde nichts geändert.",
		textAskFolderNoFiles:      "%s hat keine Datei, die ein erster Commit nehmen könnte.",
		textWhatFolderNoFiles:     "Es wurde nichts geändert. Lege zuerst die Stack-Dateien in den Ordner.",
		textAskFolderClone:        "Ein Repository nach %s klonen?",
		textWhatFolderClone:       "Als Nächstes werden die SSH-Adresse abgefragt und der Hostschlüssel gezeigt. Vor der letzten Frage ändert sich nichts.",
		textAskFolderAdopt:        "Den Klon %s mit der Adresse %s übernehmen?",
		textWhatFolderAdopt:       "Als Nächstes wird der Hostschlüssel gezeigt und der Deploy-Schlüssel des Ordners geprüft. Vor der letzten Frage ändert sich nichts.",
		textAskFolderLocal:        "Das Repository %s als lokalen Stack-Ordner übernehmen?",
		textWhatFolderApply:       "Ja nimmt die unten aufgeführten Änderungen vor.",
		textAskFolderFiles:        "In %s ein Git-Repository anlegen und übernehmen?",
		textWhatFolderFiles:       "Ja prüft den Ordner noch einmal und nimmt die unten aufgeführten Änderungen nur vor, wenn seine Liste unverändert ist.",
		textAskHostKey:            "Diesem Hostschlüssel für %s vertrauen?",
		textWhatHostKey:           "Vergleiche den Fingerabdruck zuerst mit der Git-Plattform. Noch wird nichts gespeichert; der Schlüssel wird erst nach der letzten Frage weitergegeben.",
		textWhatHostKeyPinned:     "Der Dienst vertraut genau diesem Hostschlüssel bereits. Vor der letzten Frage ändert sich nichts.",
		textAskFolderCloneFinal:   "%s nach %s klonen?",
		textWhatFolderFinal:       "Erst jetzt wird der Hostschlüssel gespeichert, und die unten aufgeführten Änderungen werden vorgenommen.",
		textFolderNoKey:           "%s hat noch keinen Deploy-Schlüssel.",
		textFolderNoKeyNext:       "Hinterlege oder erzeuge ihn auf dieser Seite und trage den öffentlichen Schlüssel bei der Git-Plattform ein, dann füge den Ordner erneut hinzu. Es wurde nichts geändert.",
		textFolderListChanged:     "%s hat sich geändert, seit die Liste gezeigt wurde; es wurde nichts geändert. Füge ihn erneut hinzu, um die neue Liste zu prüfen.",
		textFolderLocalFetch:      "Ein lokaler Stack-Ordner hat kein Remote zum Abrufen.",
		textFolderLocalKey:        "Ein lokaler Stack-Ordner hat keinen Deploy-Schlüssel.",
		textFetchFailed:           "der Abruf von %s ist fehlgeschlagen: %s",
		textAskRelease:            "Den Stack-Ordner %s lösen?",
		textWhatRelease:           "qdeploy rollt nicht mehr daraus aus. Nichts wird gelöscht und kein Container gestoppt; die Rechte, der Eintrag im systemd-Drop-in und der Deploy-Schlüssel bleiben.",
		textDoneReleased:          "Der Stack-Ordner %s ist gelöst.",
		textDoneFolderAdded:       "Der Stack-Ordner %s ist hinzugefügt.",
		textDetailKind:            "Art: %s",
		textDetailFiles:           "Dateien des ersten Commits: %d",
		textKeptChanges:           "%s; Gruppe und Modus von %d Pfaden bleiben geändert",
		textErrHost:               "auf dem Host fehlgeschlagen: %s",
	}
}
