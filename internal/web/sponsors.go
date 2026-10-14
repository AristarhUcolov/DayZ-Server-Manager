// Copyright (c) 2026 Aristarh Ucolov.
//
// Sponsors — people who chipped in to keep the manager growing. Shown on the
// Sponsors page as a thank-you and a nudge for others. The message and reply
// are testimonials, kept verbatim in whatever language they were written; only
// the surrounding UI chrome is translated.
//
// To add a sponsor: append an entry below. Newest first.
package web

import "net/http"

// Sponsor is one supporter entry.
type Sponsor struct {
	Name    string `json:"name"`              // display name, or "Аноним" / "Anonymous"
	Amount  string `json:"amount,omitempty"`  // e.g. "500 ₽"
	Date    string `json:"date,omitempty"`    // free-form, e.g. "2026-08"
	Message string `json:"message,omitempty"` // what the sponsor wrote, verbatim (original language)
	Reply   string `json:"reply,omitempty"`   // the developer's reply, verbatim (original language)
	// Optional per-language translations (lang code -> text). When the current UI
	// language is present here the page shows it; otherwise it falls back to the
	// verbatim Message / Reply above. Add these only for sponsors you translate.
	MessageI18n map[string]string `json:"messageI18n,omitempty"`
	ReplyI18n   map[string]string `json:"replyI18n,omitempty"`
	Link        string            `json:"link,omitempty"` // Steam / social, when the sponsor gave one
	Anon        bool              `json:"anon,omitempty"` // shown as anonymous; can claim a real name
}

// sponsors is the supporter roll, newest first.
var sponsors = []Sponsor{
	{
		Name:   "RFAndrio93rus",
		Amount: "1000 ₽",
		Date:   "2026-10",
		Reply:  "Огромное спасибо за щедрую поддержку! 🙏 Такой вклад по-настоящему двигает Менеджер вперёд — в этом обновлении появились вход по паролю, удалённый доступ, архив чата и много новых языков. Будем и дальше делать Менеджер лучше! ❤️",
		ReplyI18n: map[string]string{
			"en":    "Huge thanks for the generous support! 🙏 Contributions like this really move the Manager forward — this update brings password login, remote access, a chat archive and many new languages. We'll keep making the Manager better! ❤️",
			"de":    "Vielen herzlichen Dank für die großzügige Unterstützung! 🙏 Solche Beiträge bringen den Manager wirklich voran — dieses Update bringt Passwort-Anmeldung, Fernzugriff, ein Chat-Archiv und viele neue Sprachen. Wir machen den Manager weiter besser! ❤️",
			"fr":    "Un immense merci pour ce soutien généreux ! 🙏 Des contributions comme celle-ci font vraiment avancer le Manager : cette mise à jour apporte la connexion par mot de passe, l'accès à distance, des archives du chat et de nombreuses nouvelles langues. Nous continuerons à améliorer le Manager ! ❤️",
			"es":    "¡Muchísimas gracias por el generoso apoyo! 🙏 Aportes como este impulsan de verdad el Manager: esta actualización trae inicio de sesión con contraseña, acceso remoto, un archivo del chat y muchos idiomas nuevos. ¡Seguiremos mejorando el Manager! ❤️",
			"it":    "Grazie infinite per il generoso supporto! 🙏 Contributi come questo fanno davvero crescere il Manager: questo aggiornamento porta l'accesso con password, l'accesso remoto, un archivio della chat e molte nuove lingue. Continueremo a migliorare il Manager! ❤️",
			"pt":    "Muitíssimo obrigado pelo apoio generoso! 🙏 Contributos como este fazem mesmo avançar o Manager — esta atualização traz início de sessão com palavra-passe, acesso remoto, um arquivo do chat e muitas línguas novas. Vamos continuar a melhorar o Manager! ❤️",
			"pl":    "Ogromne dzięki za hojne wsparcie! 🙏 Takie wpłaty naprawdę napędzają rozwój Managera — ta aktualizacja przynosi logowanie hasłem, zdalny dostęp, archiwum czatu i wiele nowych języków. Będziemy dalej ulepszać Managera! ❤️",
			"cs":    "Obrovské díky za štědrou podporu! 🙏 Takové příspěvky opravdu posouvají Manager dopředu — tato aktualizace přináší přihlášení heslem, vzdálený přístup, archiv chatu a mnoho nových jazyků. Budeme Manager dál vylepšovat! ❤️",
			"hu":    "Hatalmas köszönet a nagyvonalú támogatásért! 🙏 Az ilyen hozzájárulások tényleg előreviszik a kezelőt — ez a frissítés jelszavas bejelentkezést, távoli elérést, chatarchívumot és sok új nyelvet hoz. A kezelőt továbbra is fejlesztjük! ❤️",
			"nl":    "Enorm bedankt voor de gulle steun! 🙏 Bijdragen zoals deze brengen de manager echt verder — deze update brengt inloggen met een wachtwoord, toegang op afstand, een chatarchief en veel nieuwe talen. We blijven de manager beter maken! ❤️",
			"sv":    "Stort tack för det generösa stödet! 🙏 Sådana bidrag tar verkligen managern framåt — den här uppdateringen ger inloggning med lösenord, fjärråtkomst, ett chattarkiv och många nya språk. Vi fortsätter att göra managern bättre! ❤️",
			"md":    "Mulțumesc enorm pentru sprijinul generos! 🙏 Asemenea contribuții chiar duc Managerul înainte — această actualizare aduce autentificarea cu parolă, accesul la distanță, o arhivă a chatului și multe limbi noi. Vom continua să facem Managerul mai bun! ❤️",
			"tr":    "Cömert desteğin için çok teşekkürler! 🙏 Bu tür katkılar Yönetici'yi gerçekten ileri taşıyor — bu güncelleme şifreyle giriş, uzaktan erişim, sohbet arşivi ve birçok yeni dil getiriyor. Yönetici'yi daha da iyi yapmaya devam edeceğiz! ❤️",
			"ar":    "شكرًا جزيلًا على هذا الدعم السخي! 🙏 مثل هذه المساهمات تدفع المدير إلى الأمام حقًا — يجلب هذا التحديث تسجيل الدخول بكلمة مرور، والوصول عن بُعد، وأرشيف الدردشة، والعديد من اللغات الجديدة. سنواصل تحسين المدير! ❤️",
			"hi":    "इस उदार समर्थन के लिए बहुत-बहुत धन्यवाद! 🙏 ऐसे योगदान सच में मैनेजर को आगे बढ़ाते हैं — इस अपडेट में पासवर्ड से लॉगिन, रिमोट एक्सेस, चैट आर्काइव और कई नई भाषाएँ आई हैं। हम मैनेजर को और बेहतर बनाते रहेंगे! ❤️",
			"th":    "ขอบคุณมากสำหรับการสนับสนุนอย่างใจกว้าง! 🙏 การสนับสนุนแบบนี้ช่วยผลักดันให้ Manager ก้าวหน้าอย่างแท้จริง — อัปเดตนี้มาพร้อมการเข้าสู่ระบบด้วยรหัสผ่าน การเข้าถึงจากระยะไกล คลังเก็บแชท และภาษาใหม่อีกมากมาย เราจะพัฒนา Manager ให้ดียิ่งขึ้นต่อไป! ❤️",
			"vi":    "Cảm ơn bạn rất nhiều vì sự ủng hộ hào phóng! 🙏 Những đóng góp như thế này thực sự giúp Manager tiến lên — bản cập nhật này mang đến đăng nhập bằng mật khẩu, truy cập từ xa, kho lưu trữ trò chuyện và nhiều ngôn ngữ mới. Chúng tôi sẽ tiếp tục làm Manager tốt hơn! ❤️",
			"id":    "Terima kasih banyak atas dukungannya yang murah hati! 🙏 Dukungan seperti ini benar-benar memajukan Manager — pembaruan ini menghadirkan login dengan kata sandi, akses jarak jauh, arsip obrolan, dan banyak bahasa baru. Kami akan terus membuat Manager lebih baik! ❤️",
			"zh":    "非常感谢你的慷慨支持！🙏 这样的支持真正推动了管理器的发展——这次更新带来了密码登录、远程访问、聊天存档以及许多新语言。我们会继续把管理器做得更好！❤️",
			"zh-tw": "非常感謝你的慷慨支持！🙏 這樣的支持真正推動了管理器的發展——這次更新帶來了密碼登入、遠端存取、聊天封存以及許多新語言。我們會繼續把管理器做得更好！❤️",
			"ja":    "寛大なご支援、本当にありがとうございます！🙏 こうしたご支援がマネージャーの開発を大きく前進させます。今回のアップデートでは、パスワードによるログイン、リモートアクセス、チャットのアーカイブ、そして多くの新しい言語を追加しました。これからもマネージャーをより良くしていきます！❤️",
			"ko":    "아낌없는 지원에 진심으로 감사드립니다! 🙏 이런 후원은 매니저 발전에 큰 힘이 됩니다 — 이번 업데이트로 비밀번호 로그인, 원격 접속, 채팅 기록 보관, 그리고 많은 새 언어가 추가되었습니다. 앞으로도 매니저를 더 좋게 만들겠습니다! ❤️",
		},
	},
	{
		Name:    "Red Linux",
		Amount:  "500 ₽",
		Date:    "2026-09",
		Message: "Эта программа крайне важна для прогресса проекта на операционной системе Linux",
		Reply:   "Огромное спасибо за поддержку! 🐧 Linux для нас в приоритете — в этом обновлении появились две сборки для Linux (в браузере и в отдельном окне), автоматическое приведение имён файлов модов к нижнему регистру и корректная остановка сервера. Будем и дальше развивать Менеджер для Linux! ❤️",
		MessageI18n: map[string]string{
			"en": "This program is extremely important for the progress of the project on the Linux operating system",
			"de": "Dieses Programm ist für den Fortschritt des Projekts auf dem Betriebssystem Linux äußerst wichtig",
			"es": "Este programa es sumamente importante para el progreso del proyecto en el sistema operativo Linux",
			"fr": "Ce programme est extrêmement important pour la progression du projet sur le système d'exploitation Linux",
			"it": "Questo programma è estremamente importante per il progresso del progetto sul sistema operativo Linux",
			"ja": "このプログラムは、Linux オペレーティングシステムでのプロジェクトの進展にとって非常に重要です",
			"ko": "이 프로그램은 Linux 운영체제에서 프로젝트가 발전하는 데 매우 중요합니다",
			"md": "Acest program este extrem de important pentru progresul proiectului pe sistemul de operare Linux",
			"pt": "Este programa é extremamente importante para o progresso do projeto no sistema operativo Linux",
			"zh": "这个程序对项目在 Linux 操作系统上的发展极其重要",
		},
		ReplyI18n: map[string]string{
			"en": "Huge thanks for the support! 🐧 Linux is a priority for us — this update brings two Linux builds (in the browser and in its own window), automatic lowercasing of mod file names and clean server shutdowns. We'll keep developing the Manager for Linux! ❤️",
			"de": "Vielen herzlichen Dank für die Unterstützung! 🐧 Linux hat für uns Priorität — dieses Update bringt zwei Linux-Versionen (im Browser und im eigenen Fenster), automatische Kleinschreibung der Mod-Dateinamen und sauberes Herunterfahren des Servers. Wir entwickeln den Manager für Linux weiter! ❤️",
			"es": "¡Muchísimas gracias por el apoyo! 🐧 Linux es una prioridad para nosotros: esta actualización trae dos versiones para Linux (en el navegador y en ventana propia), el paso automático a minúsculas de los nombres de archivos de mods y apagados limpios del servidor. ¡Seguiremos desarrollando el Manager para Linux! ❤️",
			"fr": "Un immense merci pour le soutien ! 🐧 Linux est une priorité pour nous : cette mise à jour apporte deux versions Linux (dans le navigateur et dans une fenêtre dédiée), la mise en minuscules automatique des noms de fichiers des mods et des arrêts propres du serveur. Nous continuerons à développer le Manager pour Linux ! ❤️",
			"it": "Grazie infinite per il supporto! 🐧 Linux per noi è una priorità: questo aggiornamento porta due versioni per Linux (nel browser e in una finestra dedicata), la conversione automatica in minuscolo dei nomi dei file delle mod e arresti puliti del server. Continueremo a sviluppare il Manager per Linux! ❤️",
			"ja": "ご支援本当にありがとうございます！🐧 Linux は私たちにとって優先事項です。今回のアップデートでは Linux 向けの 2 つの版（ブラウザー版と専用ウィンドウ版）、MOD ファイル名の自動小文字化、サーバーの正常停止を追加しました。これからも Linux 向けにマネージャーを開発していきます！❤️",
			"ko": "지원해 주셔서 정말 감사합니다! 🐧 Linux는 저희에게 우선순위입니다 — 이번 업데이트로 Linux용 두 가지 버전(브라우저와 전용 창), 모드 파일 이름 자동 소문자 변환, 서버 정상 종료가 추가되었습니다. 앞으로도 Linux용 매니저를 계속 발전시키겠습니다! ❤️",
			"md": "Mulțumesc enorm pentru sprijin! 🐧 Linux este o prioritate pentru noi — această actualizare aduce două versiuni pentru Linux (în browser și în fereastră proprie), trecerea automată la litere mici a numelor fișierelor de moduri și opriri curate ale serverului. Vom continua să dezvoltăm Managerul pentru Linux! ❤️",
			"pt": "Muitíssimo obrigado pelo apoio! 🐧 O Linux é uma prioridade para nós — esta atualização traz duas versões para Linux (no navegador e em janela própria), a conversão automática para minúsculas dos nomes dos ficheiros dos mods e paragens limpas do servidor. Vamos continuar a desenvolver o Manager para Linux! ❤️",
			"zh": "非常感谢你的支持！🐧 Linux 是我们的重点——这次更新带来了两个 Linux 版本（浏览器版和独立窗口版）、模组文件名自动转为小写，以及服务器的正常关闭。我们会继续为 Linux 完善管理器！❤️",
		},
		Link: "https://vk.com/red_linux",
	},
	{
		Name:    "Papa",
		Amount:  "200 ₽",
		Date:    "2026-09",
		Message: "спасибо, очень удобно пользоваться, надеюсь в дальнейшем менеджер будет процветать!",
		Reply:   "Спасибо большое за тёплые слова и поддержку! 🙏 Очень приятно, что Менеджером удобно пользоваться. Будем стараться, чтобы он и дальше процветал и становился только лучше! ❤️",
		MessageI18n: map[string]string{
			"en": "thank you, it's very convenient to use, I hope the manager will keep flourishing!",
			"de": "danke, sehr bequem zu benutzen, ich hoffe, der Manager wird auch künftig florieren!",
			"es": "gracias, es muy cómodo de usar, ¡espero que el manager siga prosperando!",
			"fr": "merci, très pratique à utiliser, j'espère que le manager continuera à prospérer !",
			"it": "grazie, è molto comodo da usare, spero che il manager continui a prosperare!",
			"ja": "ありがとう、とても使いやすいです。これからもマネージャーが発展していくことを願っています！",
			"ko": "감사합니다, 정말 편하게 쓰고 있어요. 앞으로도 매니저가 번창하길 바랍니다!",
			"md": "mulțumesc, este foarte comod de folosit, sper ca managerul să prospere și în continuare!",
			"pt": "obrigado, é muito cómodo de usar, espero que o manager continue a prosperar!",
			"zh": "谢谢，用起来非常方便，希望管理器今后越来越好！",
		},
		ReplyI18n: map[string]string{
			"en": "Thank you so much for the kind words and the support! 🙏 It's really nice to hear the Manager is convenient to use. We'll do our best to keep it flourishing and getting better and better! ❤️",
			"de": "Vielen Dank für die netten Worte und die Unterstützung! 🙏 Es freut mich sehr, dass sich der Manager bequem benutzen lässt. Wir geben unser Bestes, damit er weiter floriert und immer besser wird! ❤️",
			"es": "¡Muchas gracias por las amables palabras y el apoyo! 🙏 Me alegra mucho que el Manager resulte cómodo de usar. ¡Haremos lo posible para que siga prosperando y sea cada vez mejor! ❤️",
			"fr": "Merci beaucoup pour ces mots chaleureux et pour le soutien ! 🙏 Ça fait vraiment plaisir que le Manager soit pratique à utiliser. Nous ferons de notre mieux pour qu'il continue de prospérer et de s'améliorer ! ❤️",
			"it": "Grazie mille per le belle parole e il supporto! 🙏 Fa davvero piacere che il Manager sia comodo da usare. Faremo del nostro meglio perché continui a prosperare e a migliorare sempre di più! ❤️",
			"ja": "温かいお言葉とご支援、本当にありがとうございます！🙏 マネージャーが使いやすいと言っていただけてとても嬉しいです。これからも発展し、さらに良くなるよう頑張ります！❤️",
			"ko": "따뜻한 말씀과 지원 정말 감사합니다! 🙏 매니저가 편하다고 해 주셔서 정말 기쁩니다. 앞으로도 번창하고 점점 더 좋아지도록 최선을 다하겠습니다! ❤️",
			"md": "Mulțumesc mult pentru cuvintele calde și pentru sprijin! 🙏 Mă bucur mult că Managerul este comod de folosit. Vom face tot posibilul ca el să prospere în continuare și să devină tot mai bun! ❤️",
			"pt": "Muito obrigado pelas palavras simpáticas e pelo apoio! 🙏 É muito bom saber que o Manager é cómodo de usar. Vamos fazer o nosso melhor para que continue a prosperar e a ficar cada vez melhor! ❤️",
			"zh": "非常感谢你的暖心话语和支持！🙏 很高兴管理器用起来顺手。我们会继续努力，让它不断发展、越来越好！❤️",
		},
	},
	{
		Name:    "Yourok",
		Amount:  "$20",
		Date:    "2026-08",
		Message: "На сколько проще и легче стало делать свой сервер. Спасибо тебе добрый человек ;)",
		Reply:   "Спасибо вам большое за поддержку! 🙏 Это действительно очень мотивирует продолжать развивать Менеджер и делать его ещё лучше. Очень рад, что Менеджер оказался для вас полезным! Желаю приятного пользования и ещё раз огромное спасибо! ❤️",
		MessageI18n: map[string]string{
			"en": "How much simpler and easier it's become to run my own server. Thank you, kind soul ;)",
			"de": "Wie viel einfacher und leichter es geworden ist, einen eigenen Server zu betreiben. Danke, guter Mensch ;)",
			"es": "Qué fácil y sencillo se ha vuelto montar tu propio servidor. Gracias, buena persona ;)",
			"fr": "Comme c'est devenu plus simple et plus facile de gérer son propre serveur. Merci, brave homme ;)",
			"it": "Quanto è diventato più semplice e facile gestire il proprio server. Grazie, brava persona ;)",
			"ja": "自分のサーバーを立てるのが本当に簡単で楽になりました。ありがとう、優しい人 ;)",
			"ko": "내 서버를 운영하는 게 훨씬 간단하고 쉬워졌어요. 고마워요, 착한 분 ;)",
			"md": "Cât de simplu și ușor a devenit să-ți faci propriul server. Mulțumesc, om bun ;)",
			"pt": "Como ficou mais simples e fácil montar o próprio servidor. Obrigado, boa pessoa ;)",
			"zh": "搭建自己的服务器变得如此简单轻松。谢谢你，好心人 ;)",
		},
		ReplyI18n: map[string]string{
			"en": "Thank you so much for your support! 🙏 It truly motivates me to keep developing the Manager and make it even better. I'm really glad the Manager turned out useful for you! Enjoy using it, and once again — a huge thank you! ❤️",
			"de": "Vielen Dank für deine Unterstützung! 🙏 Das motiviert wirklich, den Manager weiterzuentwickeln und ihn noch besser zu machen. Ich freue mich sehr, dass dir der Manager nützlich ist! Viel Freude damit und nochmals herzlichen Dank! ❤️",
			"es": "¡Muchas gracias por tu apoyo! 🙏 De verdad motiva a seguir desarrollando el Manager y hacerlo aún mejor. ¡Me alegra mucho que el Manager te resulte útil! Que lo disfrutes y, una vez más, ¡muchísimas gracias! ❤️",
			"fr": "Merci beaucoup pour ton soutien ! 🙏 Ça motive vraiment à continuer de développer le Manager et à l'améliorer encore. Je suis très content que le Manager te soit utile ! Bonne utilisation et encore un immense merci ! ❤️",
			"it": "Grazie mille per il tuo supporto! 🙏 Motiva davvero a continuare a sviluppare il Manager e a renderlo ancora migliore. Sono molto contento che il Manager ti sia utile! Buon utilizzo e ancora un enorme grazie! ❤️",
			"ja": "ご支援ありがとうございます！🙏 マネージャーの開発を続け、さらに良くする大きな励みになります。マネージャーがお役に立てて本当に嬉しいです！どうぞお楽しみください、改めて本当にありがとうございます！❤️",
			"ko": "지원해 주셔서 정말 감사합니다! 🙏 매니저를 계속 개발하고 더 좋게 만드는 데 큰 힘이 됩니다. 매니저가 유용하게 쓰이신다니 정말 기쁩니다! 즐겁게 사용하시고, 다시 한 번 진심으로 감사드립니다! ❤️",
			"md": "Mulțumesc mult pentru sprijin! 🙏 Chiar motivează să dezvolt Managerul în continuare și să-l fac și mai bun. Mă bucur mult că Managerul ți-a fost util! Folosire plăcută și încă o dată un imens mulțumesc! ❤️",
			"pt": "Muito obrigado pelo teu apoio! 🙏 Isto motiva mesmo a continuar a desenvolver o Manager e a torná-lo ainda melhor. Fico muito feliz que o Manager te seja útil! Bom proveito e, mais uma vez, um enorme obrigado! ❤️",
			"zh": "非常感谢你的支持！🙏 这真的很激励我继续开发管理器并把它做得更好。很高兴管理器对你有用！祝使用愉快，再次万分感谢！❤️",
		},
	},
	{
		Name:    "Аноним",
		Amount:  "500 ₽",
		Date:    "2026-08",
		Message: "Менеджер хороший, надеюсь будет поддержка для Linux",
		Reply:   "Спасибо большое за поддержку Проекта, мы обязательно добавим.",
		MessageI18n: map[string]string{
			"en": "Great manager, hope there'll be Linux support",
			"de": "Guter Manager, ich hoffe auf Linux-Unterstützung",
			"es": "Buen manager, espero que haya soporte para Linux",
			"fr": "Bon manager, j'espère qu'il y aura une prise en charge de Linux",
			"it": "Bel manager, spero ci sarà il supporto per Linux",
			"ja": "良いマネージャーです。Linux対応があるといいな",
			"ko": "매니저 좋네요, Linux 지원이 있으면 좋겠어요",
			"md": "Manager bun, sper să apară suport pentru Linux",
			"pt": "Bom manager, espero que haja suporte para Linux",
			"zh": "管理器很好，希望能支持 Linux",
		},
		ReplyI18n: map[string]string{
			"en": "Thank you very much for supporting the project — we'll definitely add it.",
			"de": "Vielen Dank für die Unterstützung des Projekts — wir fügen es auf jeden Fall hinzu.",
			"es": "Muchas gracias por apoyar el proyecto — sin duda lo añadiremos.",
			"fr": "Merci beaucoup de soutenir le projet — nous l'ajouterons sans faute.",
			"it": "Grazie mille per il supporto al progetto — lo aggiungeremo senz'altro.",
			"ja": "プロジェクトへのご支援ありがとうございます。必ず追加します。",
			"ko": "프로젝트를 지원해 주셔서 감사합니다. 반드시 추가하겠습니다.",
			"md": "Mulțumim mult pentru sprijinul acordat proiectului — îl vom adăuga cu siguranță.",
			"pt": "Muito obrigado por apoiar o projeto — vamos adicioná-lo com certeza.",
			"zh": "非常感谢你对项目的支持，我们一定会加入。",
		},
		Anon: true,
	},
}

func (h *handlers) sponsorsList(w http.ResponseWriter, r *http.Request) {
	out := sponsors
	if out == nil {
		out = []Sponsor{}
	}
	writeJSON(w, map[string]interface{}{"sponsors": out})
}
