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
