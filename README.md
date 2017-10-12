About
=====

jsonstream is a tokenizer for a stream of json tokens. The main feature jsonstream provides over the standard library and other json parsers is a mechanism to stream very long string values in a json document without allocating memory linear in the size of the string.

Note that jsonstream is a tokenizer and not a parser. You will have to do the parsing yourself.
