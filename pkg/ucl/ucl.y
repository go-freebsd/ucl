// UCL grammar. The scanner in scanner.go does all of the context sensitive
// work (keys versus values, heredocs, macro parameters), so the grammar below
// only describes the shape of a document.
//
// Regenerate ucl.y.go with "go generate ./..." or "make generate".

%{
package ucl
%}

%union {
	obj *Object
	str string
	mem *member
}

%token <str> tokKey tokMacro
%token <obj> tokScalar
%token tokEq tokSep tokOpenBrace tokCloseBrace tokOpenBracket tokCloseBracket tokOpenParen tokCloseParen tokError

%type <obj> document object array value members elements params
%type <mem> member pair macro

%start document

%%

document
	: members
		{ yylex.(*lexer).result = $1 }
	| tokOpenBrace members tokCloseBrace opt_seps
		{ yylex.(*lexer).result = $2 }
	| array opt_seps
		{ yylex.(*lexer).result = $1 }
	;

opt_seps
	: /* empty */
	| opt_seps tokSep
	;

members
	: /* empty */
		{ $$ = NewObject() }
	| members tokSep
		{ $$ = $1 }
	| members member
		{ $$ = $1; yylex.(*lexer).apply($$, $2) }
	;

member
	: pair
		{ $$ = $1 }
	| macro
		{ $$ = $1 }
	;

pair
	: tokKey opt_eq value
		{ $$ = &member{key: $1, val: $3} }
	;

opt_eq
	: /* empty */
	| tokEq
	;

macro
	: tokMacro params value
		{ $$ = &member{macro: $1, params: $2, val: $3} }
	;

params
	: /* empty */
		{ $$ = nil }
	| tokOpenParen members tokCloseParen
		{ $$ = $2 }
	;

value
	: tokScalar
		{ $$ = $1 }
	| object
		{ $$ = $1 }
	| array
		{ $$ = $1 }
	;

object
	: tokOpenBrace members tokCloseBrace
		{ $$ = $2 }
	;

array
	: tokOpenBracket elements tokCloseBracket
		{ $$ = $2 }
	;

elements
	: /* empty */
		{ $$ = NewArray() }
	| elements tokSep
		{ $$ = $1 }
	| elements value
		{ $$ = $1; $$.AppendElem($2) }
	;

%%
