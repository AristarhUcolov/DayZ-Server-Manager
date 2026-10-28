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
	// Role is an i18n key for a title the project gave the sponsor (e.g. the
	// lead tester of a platform); Highlight sets the card apart on the page.
	Role      string `json:"role,omitempty"`
	Highlight bool   `json:"highlight,omitempty"`
}

// sponsors is the supporter roll, newest first.
var sponsors = []Sponsor{
	{
		Name:    "Аноним",
		Amount:  "200 ₽",
		Date:    "2026-10",
		Message: "Привет. Ты Красавчик. Молодец. Лаунчер чудо и видос лайк. Спасибо. По больше бы таких толковых.",
		Reply:   "Привет! Огромное спасибо за поддержку, тёплые слова и за то, что покрыли комиссию! 🙏 Очень рад, что Менеджер и видео понравились. В этом обновлении — автозапуск вместе с компьютером, смена доступа к панели без перезапуска, карта Nasdara и много исправлений. Будем и дальше делать Менеджер лучше! ❤️",
		MessageI18n: map[string]string{
			"en":    "Hi. You're awesome. Well done. The launcher is a marvel, and a like for the video. Thanks. We need more sharp people like you.",
			"de":    "Hallo. Du bist der Hammer. Gut gemacht. Der Launcher ist ein Wunder, und ein Like fürs Video. Danke. Mehr solche fähigen Leute bitte.",
			"es":    "Hola. Eres un crack. Bien hecho. El launcher es una maravilla y like al vídeo. Gracias. Ojalá hubiera más gente así de capaz.",
			"fr":    "Salut. T'es un as. Bravo. Le launcher est une merveille, et un like pour la vidéo. Merci. Il faudrait plus de gens doués comme toi.",
			"it":    "Ciao. Sei un grande. Bravo. Il launcher è una meraviglia e un like al video. Grazie. Ci vorrebbero più persone in gamba così.",
			"pt":    "Olá. És um craque. Muito bem. O launcher é uma maravilha, e um like no vídeo. Obrigado. Precisávamos de mais gente competente assim.",
			"pl":    "Cześć. Jesteś gość. Brawo. Launcher to cudo, a filmik — łapka w górę. Dzięki. Więcej takich ogarniętych ludzi.",
			"cs":    "Ahoj. Jsi frajer. Dobrá práce. Launcher je zázrak a video dostává like. Díky. Víc takových šikovných lidí.",
			"hu":    "Szia. Király vagy. Szép munka. A launcher csoda, a videóra pedig egy like. Köszi. Több ilyen értelmes ember kellene.",
			"nl":    "Hoi. Je bent een topper. Goed gedaan. De launcher is een wonder en een like voor de video. Bedankt. Meer van zulke slimme mensen graag.",
			"sv":    "Hej. Du är grym. Bra jobbat. Launchern är ett under och en like på videon. Tack. Det behövs fler kunniga människor som du.",
			"md":    "Salut. Ești tare. Bravo. Launcherul e o minune, iar videoclipului — like. Mulțumesc. Mai mulți oameni pricepuți ca tine.",
			"tr":    "Selam. Harikasın. Eline sağlık. Launcher harika, videoya da bir beğeni. Teşekkürler. Senin gibi işini bilen insanlar çoğalsın.",
			"ar":    "مرحبًا. أنت رائع. أحسنت. المشغّل تحفة، ولايك للفيديو. شكرًا. نحتاج إلى المزيد من الأشخاص الأكفاء مثلك.",
			"hi":    "नमस्ते। तुम कमाल हो। शाबाश। लॉन्चर कमाल का है और वीडियो को लाइक। धन्यवाद। तुम्हारे जैसे समझदार लोग और होने चाहिए।",
			"th":    "สวัสดี คุณสุดยอดมาก เยี่ยมเลย ตัวลอนเชอร์คือสุดยอด และกดไลก์ให้วิดีโอด้วย ขอบคุณ อยากให้มีคนเก่งแบบนี้เยอะ ๆ",
			"vi":    "Chào. Bạn đỉnh lắm. Giỏi lắm. Launcher thật tuyệt và like cho video. Cảm ơn. Cần thêm nhiều người giỏi như bạn.",
			"id":    "Halo. Kamu keren. Mantap. Launcher-nya luar biasa, dan like untuk videonya. Terima kasih. Semoga makin banyak orang cerdas sepertimu.",
			"zh":    "你好。你太棒了。干得好。启动器简直是奇迹，视频点赞。谢谢。希望多一些像你这样能干的人。",
			"zh-tw": "你好。你太棒了。做得好。啟動器簡直是奇蹟，影片按讚。謝謝。希望多一些像你這樣能幹的人。",
			"ja":    "こんにちは。最高です。お見事。ランチャーはまさに奇跡、動画にはいいね。ありがとう。あなたのような有能な人がもっと増えてほしい。",
			"ko":    "안녕하세요. 정말 멋져요. 잘하셨어요. 런처는 기적이고 영상엔 좋아요. 고마워요. 이런 실력 있는 사람이 더 많았으면 좋겠어요.",
		},
		ReplyI18n: map[string]string{
			"en":    "Hi! Huge thanks for the support, the kind words and for covering the fee! 🙏 I'm really glad you liked the Manager and the video. This update brings starting together with the computer, changing panel access without a restart, a Nasdara map and lots of fixes. We'll keep making the Manager better! ❤️",
			"de":    "Hallo! Vielen herzlichen Dank für die Unterstützung, die netten Worte und dafür, dass du die Gebühr übernommen hast! 🙏 Es freut mich sehr, dass dir der Manager und das Video gefallen. Dieses Update bringt den Start zusammen mit dem Computer, das Ändern des Panel-Zugriffs ohne Neustart, eine Nasdara-Karte und viele Korrekturen. Wir machen den Manager weiter besser! ❤️",
			"es":    "¡Hola! ¡Muchísimas gracias por el apoyo, las amables palabras y por cubrir la comisión! 🙏 Me alegra mucho que te hayan gustado el Manager y el vídeo. Esta actualización trae el inicio junto con el ordenador, el cambio del acceso al panel sin reiniciar, un mapa de Nasdara y muchas correcciones. ¡Seguiremos mejorando el Manager! ❤️",
			"fr":    "Salut ! Un immense merci pour le soutien, les mots chaleureux et pour avoir couvert les frais ! 🙏 Je suis vraiment content que le Manager et la vidéo t'aient plu. Cette mise à jour apporte le démarrage avec l'ordinateur, le changement d'accès au panneau sans redémarrage, une carte de Nasdara et de nombreuses corrections. Nous continuerons à améliorer le Manager ! ❤️",
			"it":    "Ciao! Grazie infinite per il supporto, le belle parole e per aver coperto la commissione! 🙏 Sono davvero contento che il Manager e il video ti siano piaciuti. Questo aggiornamento porta l'avvio insieme al computer, il cambio dell'accesso al pannello senza riavvio, una mappa di Nasdara e molte correzioni. Continueremo a migliorare il Manager! ❤️",
			"pt":    "Olá! Muitíssimo obrigado pelo apoio, pelas palavras simpáticas e por teres coberto a comissão! 🙏 Fico muito contente por teres gostado do Manager e do vídeo. Esta atualização traz o arranque com o computador, a mudança do acesso ao painel sem reiniciar, um mapa de Nasdara e muitas correções. Vamos continuar a melhorar o Manager! ❤️",
			"pl":    "Cześć! Ogromne dzięki za wsparcie, miłe słowa i za pokrycie prowizji! 🙏 Bardzo się cieszę, że Manager i film się spodobały. Ta aktualizacja przynosi uruchamianie razem z komputerem, zmianę dostępu do panelu bez restartu, mapę Nasdary i mnóstwo poprawek. Będziemy dalej ulepszać Managera! ❤️",
			"cs":    "Ahoj! Obrovské díky za podporu, milá slova a za to, že jsi pokryl poplatek! 🙏 Moc mě těší, že se ti Manager i video líbily. Tato aktualizace přináší spouštění spolu s počítačem, změnu přístupu k panelu bez restartu, mapu Nasdary a spoustu oprav. Budeme Manager dál vylepšovat! ❤️",
			"hu":    "Szia! Hatalmas köszönet a támogatásért, a kedves szavakért és azért, hogy kifizetted a díjat! 🙏 Nagyon örülök, hogy tetszett a kezelő és a videó. Ez a frissítés hozza az indítást a számítógéppel együtt, a panel-hozzáférés újraindítás nélküli módosítását, egy Nasdara-térképet és rengeteg javítást. A kezelőt továbbra is fejlesztjük! ❤️",
			"nl":    "Hoi! Enorm bedankt voor de steun, de lieve woorden en voor het betalen van de kosten! 🙏 Ik ben echt blij dat je de manager en de video leuk vond. Deze update brengt opstarten samen met de computer, het wijzigen van de paneeltoegang zonder herstart, een kaart van Nasdara en veel verbeteringen. We blijven de manager beter maken! ❤️",
			"sv":    "Hej! Stort tack för stödet, de vänliga orden och för att du stod för avgiften! 🙏 Jag är verkligen glad att du gillade managern och videon. Den här uppdateringen ger start tillsammans med datorn, ändring av panelåtkomst utan omstart, en karta över Nasdara och många rättningar. Vi fortsätter att göra managern bättre! ❤️",
			"md":    "Salut! Mulțumesc enorm pentru sprijin, cuvintele calde și pentru că ai acoperit comisionul! 🙏 Mă bucur mult că ți-au plăcut Managerul și videoclipul. Această actualizare aduce pornirea odată cu calculatorul, schimbarea accesului la panou fără repornire, o hartă Nasdara și multe corecții. Vom continua să facem Managerul mai bun! ❤️",
			"tr":    "Selam! Destek, güzel sözler ve komisyonu karşıladığın için çok teşekkürler! 🙏 Yönetici'yi ve videoyu beğenmene çok sevindim. Bu güncelleme bilgisayarla birlikte başlatma, panel erişimini yeniden başlatmadan değiştirme, bir Nasdara haritası ve birçok düzeltme getiriyor. Yönetici'yi daha da iyi yapmaya devam edeceğiz! ❤️",
			"ar":    "مرحبًا! شكرًا جزيلًا على الدعم والكلمات اللطيفة وعلى تغطية الرسوم! 🙏 سعيد جدًا بأن المدير والفيديو نالا إعجابك. يجلب هذا التحديث التشغيل مع الحاسوب، وتغيير الوصول إلى اللوحة دون إعادة تشغيل، وخريطة Nasdara، والكثير من الإصلاحات. سنواصل تحسين المدير! ❤️",
			"hi":    "नमस्ते! समर्थन, अच्छे शब्दों और शुल्क चुकाने के लिए बहुत-बहुत धन्यवाद! 🙏 मुझे बहुत खुशी है कि आपको मैनेजर और वीडियो पसंद आए। इस अपडेट में कंप्यूटर के साथ शुरू होना, बिना रीस्टार्ट के पैनल एक्सेस बदलना, Nasdara का नक्शा और बहुत सारे सुधार हैं। हम मैनेजर को और बेहतर बनाते रहेंगे! ❤️",
			"th":    "สวัสดี! ขอบคุณมากสำหรับการสนับสนุน คำพูดดี ๆ และที่ช่วยจ่ายค่าธรรมเนียมให้! 🙏 ดีใจมากที่คุณชอบ Manager และวิดีโอ อัปเดตนี้มาพร้อมการเริ่มทำงานพร้อมคอมพิวเตอร์ การเปลี่ยนสิทธิ์เข้าถึงแผงควบคุมโดยไม่ต้องรีสตาร์ต แผนที่ Nasdara และการแก้ไขอีกมากมาย เราจะพัฒนา Manager ให้ดียิ่งขึ้นต่อไป! ❤️",
			"vi":    "Chào bạn! Cảm ơn rất nhiều vì sự ủng hộ, những lời tốt đẹp và vì đã trả phí giúp! 🙏 Mình rất vui vì bạn thích Manager và video. Bản cập nhật này mang đến khởi động cùng máy tính, đổi quyền truy cập bảng điều khiển không cần khởi động lại, bản đồ Nasdara và rất nhiều bản sửa lỗi. Chúng tôi sẽ tiếp tục làm Manager tốt hơn! ❤️",
			"id":    "Halo! Terima kasih banyak atas dukungan, kata-kata baiknya, dan sudah menanggung biayanya! 🙏 Senang sekali kamu suka Manager dan videonya. Pembaruan ini menghadirkan mulai bersama komputer, ganti akses panel tanpa mulai ulang, peta Nasdara, dan banyak perbaikan. Kami akan terus membuat Manager lebih baik! ❤️",
			"zh":    "你好！非常感谢你的支持、暖心的话，还帮忙承担了手续费！🙏 很高兴你喜欢管理器和视频。这次更新带来了随电脑一起启动、无需重启即可更改面板访问方式、Nasdara 地图以及大量修复。我们会继续把管理器做得更好！❤️",
			"zh-tw": "你好！非常感謝你的支持、暖心的話，還幫忙負擔了手續費！🙏 很高興你喜歡管理器和影片。這次更新帶來了隨電腦一起啟動、不必重新啟動即可變更面板存取方式、Nasdara 地圖以及大量修正。我們會繼續把管理器做得更好！❤️",
			"ja":    "こんにちは！ご支援と温かいお言葉、そして手数料までご負担いただき本当にありがとうございます！🙏 マネージャーと動画を気に入っていただけてとても嬉しいです。今回のアップデートでは、パソコンと一緒の自動起動、再起動なしでのパネルのアクセス設定変更、Nasdara のマップ、そして多くの修正を追加しました。これからもマネージャーをより良くしていきます！❤️",
			"ko":    "안녕하세요! 후원과 따뜻한 말씀, 그리고 수수료까지 부담해 주셔서 정말 감사합니다! 🙏 매니저와 영상을 마음에 들어 하셔서 정말 기쁩니다. 이번 업데이트에는 컴퓨터와 함께 자동 시작, 재시작 없이 패널 접근 방식 변경, Nasdara 지도, 그리고 많은 수정이 담겼습니다. 앞으로도 매니저를 더 좋게 만들겠습니다! ❤️",
		},
		Anon: true,
	},
	{
		Name:   "Аноним",
		Amount: "1000 ₽",
		Date:   "2026-10",
		Reply:  "Огромное спасибо за щедрую поддержку и за то, что покрыли комиссию! 🙏 Такой вклад очень помогает развивать Менеджер — в этом обновлении исправлены стартовый набор игроков и живая карта, добавлены карта Nasdara, автозапуск и многое другое. Будем и дальше делать Менеджер лучше! ❤️",
		ReplyI18n: map[string]string{
			"en":    "Huge thanks for the generous support and for covering the fee! 🙏 Contributions like this really help the Manager grow — this update fixes the player loadout and the live map and adds a Nasdara map, autostart and much more. We'll keep making the Manager better! ❤️",
			"de":    "Vielen herzlichen Dank für die großzügige Unterstützung und dafür, dass du die Gebühr übernommen hast! 🙏 Solche Beiträge helfen dem Manager wirklich beim Wachsen — dieses Update repariert die Spieler-Ausrüstung und die Live-Karte und bringt eine Nasdara-Karte, Autostart und vieles mehr. Wir machen den Manager weiter besser! ❤️",
			"es":    "¡Muchísimas gracias por el generoso apoyo y por cubrir la comisión! 🙏 Aportes como este ayudan de verdad a que el Manager crezca: esta actualización arregla el equipo inicial de los jugadores y el mapa en vivo, y añade un mapa de Nasdara, el inicio automático y mucho más. ¡Seguiremos mejorando el Manager! ❤️",
			"fr":    "Un immense merci pour ce soutien généreux et pour avoir couvert les frais ! 🙏 Des contributions comme celle-ci aident vraiment le Manager à grandir : cette mise à jour corrige l'équipement de départ des joueurs et la carte en direct, et ajoute une carte de Nasdara, le démarrage automatique et bien plus. Nous continuerons à améliorer le Manager ! ❤️",
			"it":    "Grazie infinite per il generoso supporto e per aver coperto la commissione! 🙏 Contributi come questo aiutano davvero il Manager a crescere: questo aggiornamento sistema l'equipaggiamento iniziale dei giocatori e la mappa dal vivo, e aggiunge una mappa di Nasdara, l'avvio automatico e molto altro. Continueremo a migliorare il Manager! ❤️",
			"pt":    "Muitíssimo obrigado pelo apoio generoso e por teres coberto a comissão! 🙏 Contributos como este ajudam mesmo o Manager a crescer — esta atualização corrige o equipamento inicial dos jogadores e o mapa ao vivo, e acrescenta um mapa de Nasdara, o arranque automático e muito mais. Vamos continuar a melhorar o Manager! ❤️",
			"pl":    "Ogromne dzięki za hojne wsparcie i za pokrycie prowizji! 🙏 Takie wpłaty naprawdę pomagają Managerowi rosnąć — ta aktualizacja naprawia ekwipunek startowy graczy i mapę na żywo oraz dodaje mapę Nasdary, autostart i wiele więcej. Będziemy dalej ulepszać Managera! ❤️",
			"cs":    "Obrovské díky za štědrou podporu a za pokrytí poplatku! 🙏 Takové příspěvky opravdu pomáhají Manageru růst — tato aktualizace opravuje startovní výbavu hráčů a živou mapu a přidává mapu Nasdary, automatické spouštění a mnohem víc. Budeme Manager dál vylepšovat! ❤️",
			"hu":    "Hatalmas köszönet a nagyvonalú támogatásért és azért, hogy kifizetted a díjat! 🙏 Az ilyen hozzájárulások tényleg segítik a kezelő fejlődését — ez a frissítés kijavítja a játékosok kezdőfelszerelését és az élő térképet, és hoz egy Nasdara-térképet, automatikus indítást és még sok mást. A kezelőt továbbra is fejlesztjük! ❤️",
			"nl":    "Enorm bedankt voor de gulle steun en voor het betalen van de kosten! 🙏 Bijdragen zoals deze helpen de manager echt groeien — deze update repareert de startuitrusting van spelers en de live-kaart, en voegt een kaart van Nasdara, automatisch starten en nog veel meer toe. We blijven de manager beter maken! ❤️",
			"sv":    "Stort tack för det generösa stödet och för att du stod för avgiften! 🙏 Sådana bidrag hjälper verkligen managern att växa — den här uppdateringen rättar spelarnas startutrustning och livekartan och lägger till en karta över Nasdara, autostart och mycket mer. Vi fortsätter att göra managern bättre! ❤️",
			"md":    "Mulțumesc enorm pentru sprijinul generos și pentru că ai acoperit comisionul! 🙏 Asemenea contribuții chiar ajută Managerul să crească — această actualizare repară echipamentul de start al jucătorilor și harta live și adaugă o hartă Nasdara, pornirea automată și multe altele. Vom continua să facem Managerul mai bun! ❤️",
			"tr":    "Cömert desteğin ve komisyonu karşıladığın için çok teşekkürler! 🙏 Bu tür katkılar Yönetici'nin büyümesine gerçekten yardım ediyor — bu güncelleme oyuncuların başlangıç teçhizatını ve canlı haritayı düzeltiyor; bir Nasdara haritası, otomatik başlatma ve çok daha fazlasını ekliyor. Yönetici'yi daha da iyi yapmaya devam edeceğiz! ❤️",
			"ar":    "شكرًا جزيلًا على هذا الدعم السخي وعلى تغطية الرسوم! 🙏 مثل هذه المساهمات تساعد المدير على النمو حقًا — يصلح هذا التحديث العتاد الابتدائي للاعبين والخريطة المباشرة، ويضيف خريطة Nasdara والتشغيل التلقائي والكثير غير ذلك. سنواصل تحسين المدير! ❤️",
			"hi":    "इस उदार समर्थन और शुल्क चुकाने के लिए बहुत-बहुत धन्यवाद! 🙏 ऐसे योगदान सच में मैनेजर को आगे बढ़ने में मदद करते हैं — यह अपडेट खिलाड़ियों का शुरुआती सामान और लाइव मैप ठीक करता है, और Nasdara का नक्शा, अपने-आप शुरू होना और बहुत कुछ जोड़ता है। हम मैनेजर को और बेहतर बनाते रहेंगे! ❤️",
			"th":    "ขอบคุณมากสำหรับการสนับสนุนอย่างใจกว้างและที่ช่วยจ่ายค่าธรรมเนียมให้! 🙏 การสนับสนุนแบบนี้ช่วยให้ Manager เติบโตอย่างแท้จริง — อัปเดตนี้แก้ไขชุดอุปกรณ์เริ่มต้นของผู้เล่นและแผนที่สด และเพิ่มแผนที่ Nasdara การเริ่มอัตโนมัติ และอีกมากมาย เราจะพัฒนา Manager ให้ดียิ่งขึ้นต่อไป! ❤️",
			"vi":    "Cảm ơn bạn rất nhiều vì sự ủng hộ hào phóng và vì đã trả phí giúp! 🙏 Những đóng góp như thế này thực sự giúp Manager phát triển — bản cập nhật này sửa trang bị khởi đầu của người chơi và bản đồ trực tiếp, đồng thời thêm bản đồ Nasdara, tự khởi động và nhiều hơn nữa. Chúng tôi sẽ tiếp tục làm Manager tốt hơn! ❤️",
			"id":    "Terima kasih banyak atas dukungannya yang murah hati dan sudah menanggung biayanya! 🙏 Dukungan seperti ini benar-benar membantu Manager berkembang — pembaruan ini memperbaiki perlengkapan awal pemain dan peta langsung, serta menambahkan peta Nasdara, mulai otomatis, dan banyak lagi. Kami akan terus membuat Manager lebih baik! ❤️",
			"zh":    "非常感谢你的慷慨支持，还帮忙承担了手续费！🙏 这样的支持真正帮助管理器成长——这次更新修复了玩家初始装备和实时地图，并加入了 Nasdara 地图、开机自启等许多内容。我们会继续把管理器做得更好！❤️",
			"zh-tw": "非常感謝你的慷慨支持，還幫忙負擔了手續費！🙏 這樣的支持真正幫助管理器成長——這次更新修正了玩家初始裝備和即時地圖，並加入了 Nasdara 地圖、開機自動啟動等許多內容。我們會繼續把管理器做得更好！❤️",
			"ja":    "寛大なご支援と、手数料までご負担いただき本当にありがとうございます！🙏 こうしたご支援がマネージャーの成長を大きく支えています。今回のアップデートでは、プレイヤーの初期装備とライブマップを修正し、Nasdara のマップや自動起動などを追加しました。これからもマネージャーをより良くしていきます！❤️",
			"ko":    "아낌없는 후원과 수수료까지 부담해 주셔서 정말 감사합니다! 🙏 이런 후원은 매니저가 성장하는 데 큰 힘이 됩니다 — 이번 업데이트로 플레이어 시작 장비와 라이브 지도를 고치고, Nasdara 지도와 자동 시작 등 많은 기능을 추가했습니다. 앞으로도 매니저를 더 좋게 만들겠습니다! ❤️",
		},
		Anon: true,
	},
	{
		Name:      "RFAndrio93rus",
		Amount:    "1000 ₽",
		Date:      "2026-10",
		Link:      "https://steamcommunity.com/profiles/76561198183722125/",
		Role:      "sponsors.role.testerWindows",
		Highlight: true,
		Reply:     "Огромное спасибо за щедрую поддержку! 🙏 Такой вклад по-настоящему двигает Менеджер вперёд — в этом обновлении появились вход по паролю, удалённый доступ, архив чата и много новых языков. Будем и дальше делать Менеджер лучше! ❤️",
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
		Name:      "Red Linux",
		Amount:    "500 ₽",
		Date:      "2026-09",
		Role:      "sponsors.role.testerLinux",
		Highlight: true,
		Message:   "Эта программа крайне важна для прогресса проекта на операционной системе Linux",
		Reply:     "Огромное спасибо за поддержку! 🐧 Linux для нас в приоритете — в этом обновлении появились две сборки для Linux (в браузере и в отдельном окне), автоматическое приведение имён файлов модов к нижнему регистру и корректная остановка сервера. Будем и дальше развивать Менеджер для Linux! ❤️",
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
