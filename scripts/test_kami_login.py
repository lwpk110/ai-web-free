import base64
import http.server
import json
import os
import tempfile
import threading
import unittest

import kami_login


class TestKamiLogin(unittest.TestCase):
    def test_generate_totp_length_and_digits(self):
        code = kami_login.generate_totp("OPWOLPW6KQMM6K4V674HAIE6YS2L56NR")
        self.assertEqual(len(code), 6)
        self.assertTrue(code.isdigit())

    def test_parse_kami_file_and_build_json(self):
        content = (
            "# comment\n"
            "\n"
            "a@example.com----pass1----OPWOLPW6KQMM6K4V674HAIE6YS2L56NR\n"
            "b@example.com----pass2----5M22V32CLCU472EZKS27J7JWPWGZAUXV\n"
        )
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False, encoding="utf-8") as f:
            f.write(content)
            path = f.name
        try:
            accounts = kami_login.parse_kami_file(path)
            self.assertEqual(len(accounts), 2)
            self.assertEqual(accounts[0]["email"], "a@example.com")
            self.assertEqual(accounts[1]["totp_secret"], "5M22V32CLCU472EZKS27J7JWPWGZAUXV")

            data = kami_login.build_empty_sub2api_json(accounts)
            self.assertEqual(data["type"], "sub2api-data")
            self.assertEqual(len(data["accounts"]), 2)
            self.assertEqual(data["accounts"][0]["extra"]["password"], "pass1")
        finally:
            os.remove(path)

    def test_extract_jwt_meta(self):
        payload = {"https://api.openai.com/auth": {"chatgpt_account_id": "acct_xyz"}, "exp": 1900000000}
        b64 = base64.urlsafe_b64encode(json.dumps(payload).encode("utf-8")).decode("utf-8").rstrip("=")
        token = f"eyJhbGciOiJSUzI1NiJ9.{b64}.sig"
        acct_id, exp = kami_login._extract_jwt_meta(token)
        self.assertEqual(acct_id, "acct_xyz")
        self.assertEqual(exp, 1900000000)

    def test_push_to_ai_web_free(self):
        received = {}

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                received["path"] = self.path
                received["body"] = json.loads(self.rfile.read(length).decode("utf-8"))
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({"result": {"added": 1}}).encode("utf-8"))

            def log_message(self, format, *args):
                pass

        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        port = server.server_address[1]
        t = threading.Thread(target=server.serve_forever, daemon=True)
        t.start()
        try:
            res = kami_login.push_to_ai_web_free(
                f"http://127.0.0.1:{port}",
                {"type": "sub2api-data", "accounts": []},
            )
            self.assertEqual(received["path"], "/admin/api/v1/providers/chatgpt/accounts/import")
            self.assertEqual(received["body"]["credential_type"], "sub2api_json")
            self.assertEqual(res["result"]["added"], 1)
        finally:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
