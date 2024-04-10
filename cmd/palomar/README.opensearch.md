
# Basic OpenSearch Operations

We use OpenSearch version 2.13+, with the `analysis-icu` and `analysis-kuromoji` plugins. These are included automatically on the AWS hosted version of Opensearch, otherwise you need to install:

    sudo /usr/share/opensearch/bin/opensearch-plugin install analysis-icu
    sudo /usr/share/opensearch/bin/opensearch-plugin install analysis-kuromoji
    sudo service opensearch restart

If you are trying to use Elasticsearch 7.10 instead of OpenSearch, you can install the plugin with:

    sudo /usr/share/elasticsearch/bin/elasticsearch-plugin install analysis-icu
    sudo /usr/share/elasticsearch/bin/elasticsearch-plugin install analysis-kuromoji
    sudo service elasticsearch restart

## Local Development

With OpenSearch running locally.

To manually drop and re-build the indices with new schemas (palomar will create these automatically if they don't exist, but this can be helpful when developing the schema itself):

    http delete :9200/palomar_post
    http delete :9200/palomar_profile
    http put :9200/palomar_post < post_schema.json
    http put :9200/palomar_profile < profile_schema.json

Put a single object (good for debugging):

    head -n1 examples.json | http post :9200/palomar_post/_doc/0
    http get :9200/palomar_post/_doc/0

Bulk insert from a file on disk:

    # esbulk is a golang CLI tool which must be installed separately
    esbulk -verbose -id ident -index palomar_post -type _doc examples.json

## Index Aliases

To make re-indexing and schema changes easier, we can create versioned (or
time-stamped) elasticsearch indexes, and then point to them using index
aliases. The index alias updates are fast and atomic, so we can slowly build up
a new index and then cut over with no downtime.

    http put :9200/palomar_post_v04 < post_schema.json

To do an atomic swap from one alias to a new one ("zero downtime"):

    http post :9200/_aliases << EOF
        {
            "actions": [
                { "remove": { "index": "palomar_post_v05", "alias": "palomar_post" }},
                { "add":    { "index": "palomar_post_v06", "alias": "palomar_post" }}
            ]
        }
    EOF

To replace an existing ("real") index with an alias pointer, do two actions
(not truly zero-downtime, but pretty fast):

    http delete :9200/palomar_post
    http put :9200/palomar_post_v03/_alias/palomar_post

## Full-Text Querying

A generic full-text "query string" query look like this (replace "blood" with
actual query string, and "size" field with the max results to return):

    GET /palomar_post/_search
    {
      "query": {
        "query_string": {
          "query": "blood",
          "analyzer": "textIcuSearch",
          "default_operator": "AND",
          "analyze_wildcard": true,
          "lenient": true,
          "fields": ["handle^5", "text"]
        }
      },
      "size": 3
    }

In the results take `.hits.hits[]._source` as the objects; `.hits.total` is the
total number of search hits.


## Index Debugging

Check index size:

    http get :9200/palomar_post/_count
    http get :9200/palomar_profile/_count
