---
name: outlook_manager
version: 1.1.0
description: Windows Outlook uygulamasına tam erişim sağlar. Mailleri okur, arar ve mail gönderir.
trigger: manual
parameters:
  type: object
  properties:
    action:
      type: string
      enum: ["list", "search", "send"]
      description: "Yapılacak işlem. 'list': son mailleri getirir, 'search': kelime arar, 'send': mail gönderir."
    count:
      type: integer
      description: "list ve search eylemleri için getirilecek mail sayısı (varsayılan 10)."
    unread_only:
      type: boolean
      description: "list eyleminde sadece okunmamış mailleri getirmek için true yapın."
    query:
      type: string
      description: "search eylemi için aranacak kelime veya cümle."
    to:
      type: string
      description: "send eylemi için alıcı e-posta adresi."
    subject:
      type: string
      description: "send eylemi için mail konusu."
    body:
      type: string
      description: "send eylemi için mailin ana içeriği."
  required: ["action"]
---

# Logic
'''python
import win32com.client
import os
import sys
import json
from datetime import datetime

class OutlookManager:
    def __init__(self):
        # Dispatch kullanarak mevcut oturuma bağlanmayı dener
        self.outlook = win32com.client.Dispatch("Outlook.Application")
        self.namespace = self.outlook.GetNamespace("MAPI")
        self.inbox = self.namespace.GetDefaultFolder(6)

    # -------------------------------
    # MAIL LISTELEME
    # -------------------------------
    def list_emails(self, count=10, unread_only=False):
        messages = self.inbox.Items
        messages.Sort("[ReceivedTime]", True)

        results = []
        i = 1
        collected = 0

        while collected < count and i <= len(messages):
            try:
                msg = messages.Item(i)
                if unread_only and msg.UnRead is False:
                    i += 1
                    continue
                results.append(self._format_mail(msg))
                collected += 1
            except Exception:
                pass # Bazen bozuk mailler COM hatası verebilir, pas geçiyoruz
            i += 1

        return {"success": True, "data": results}

    # -------------------------------
    # ARAMA
    # -------------------------------
    def search(self, query, count=10):
        messages = self.inbox.Items
        messages.Sort("[ReceivedTime]", True)

        results = []
        for i in range(1, len(messages) + 1):
            try:
                msg = messages.Item(i)
                # Subject, SenderName veya Body içinde arama (None kontrolü ile)
                subj = str(msg.Subject).lower() if msg.Subject else ""
                sender = str(msg.SenderName).lower() if msg.SenderName else ""
                body = str(msg.Body).lower() if msg.Body else ""

                if query.lower() in subj or query.lower() in sender or query.lower() in body:
                    results.append(self._format_mail(msg))

                if len(results) >= count:
                    break
            except Exception:
                continue

        return {"success": True, "data": results}

    # -------------------------------
    # MAIL GÖNDERME
    # -------------------------------
    def send_mail(self, to, subject, body, attachments=None):
        if not to or not subject:
            return {"success": False, "error": "Alıcı (to) ve Konu (subject) zorunludur."}
            
        mail = self.outlook.CreateItem(0)
        mail.To = to
        mail.Subject = subject
        mail.Body = body

        if attachments:
            for file in attachments:
                if os.path.exists(file):
                    mail.Attachments.Add(file)

        mail.Send()
        return {"success": True, "message": f"Mail başarıyla gönderildi: {to}"}

    # -------------------------------
    # FORMAT
    # -------------------------------
    def _format_mail(self, msg):
        return {
            "subject": getattr(msg, 'Subject', 'Konusuz'),
            "from": getattr(msg, 'SenderName', 'Bilinmeyen'),
            "to": getattr(msg, 'To', ''),
            "received": str(getattr(msg, 'ReceivedTime', '')),
            "unread": getattr(msg, 'UnRead', False),
            "preview": str(getattr(msg, 'Body', ''))[:150].replace('\r', '').replace('\n', ' ')
        }

# ------------------------------------
# PARS ENTRYPOINT (Dinamik Yönlendirici)
# ------------------------------------
if __name__ == "__main__":
    manager = OutlookManager()
    
    # 1. Go'dan gelen JSON argümanlarını oku
    args_json = "{}"
    if len(sys.argv) > 1:
        args_json = sys.argv[1]
        
    try:
        args = json.loads(args_json)
    except Exception as e:
        print(json.dumps({"success": False, "error": f"JSON Parse Hatası: {str(e)}"}))
        sys.exit(1)

    action = args.get("action", "list")
    
    # 2. İstenen aksiyona göre class metodunu çağır ve sonucu JSON olarak bas
    try:
        if action == "list":
            count = int(args.get("count", 10))
            unread = bool(args.get("unread_only", False))
            result = manager.list_emails(count=count, unread_only=unread)
            
        elif action == "search":
            query = args.get("query", "")
            count = int(args.get("count", 10))
            result = manager.search(query=query, count=count)
            
        elif action == "send":
            result = manager.send_mail(
                to=args.get("to"), 
                subject=args.get("subject"), 
                body=args.get("body", "")
            )
        else:
            result = {"success": False, "error": f"Bilinmeyen eylem: {action}"}
            
    except Exception as e:
        result = {"success": False, "error": str(e)}

    # Pars'in (Go aracının) okuyacağı tek çıktı bu olmalı
    print(json.dumps(result))
'''