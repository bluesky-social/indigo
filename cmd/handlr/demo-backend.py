"""
Run this script with:

    FLASK_APP=demo-backend python3 -m flask run --host 127.0.0.1 --port 5000
"""

from flask import Flask, abort, request, jsonify

app = Flask(__name__)

known_handles = {
    "demo.handlr.example.com": "did:web:dummy.example.com",
}

@app.route("/", methods=['GET'])
def homepage():
    return "This is a demo HTTP server which resolves handles to DIDs"

@app.route("/xrpc/com.atproto.identity.resolveHandle", methods=['GET'])
def resolve_handle():
    handle = request.args.get('handle', '')
    if not handle:
        abort(400, "handle param not passed")
    did = known_handles.get(handle, '')
    if not did:
        abort(404, "handle not found")
    return jsonify({"did": did})
