// @target: es2015
// @jsx: preserve
// @strict: true
// @noEmit: true

// TS 5.0.2 to 7.0.2 has a bug where empty jsx expressions are not indexed
// correctly when positioning diagnostics

type Child = string | number | JSX.Element | undefined;
declare namespace JSX {
    interface Element { tag: string }
    interface ElementChildrenAttribute { children: {} }
    interface IntrinsicElements {
        div: { children?: Child | Child[] };
        br: {};
    }
}

declare const bad: { notAJsxNode: true };

// Check the error lands on {bad} not the empty expression.
const afterOne = <div>
    {/* ts 5.0.2 to 7.0.2 show the diag here */}
    {bad}
    <br />
</div>;

const afterSeveral = <div>
    {/* ts 5.0.2 to 7.0.2 show the 1st diag here */}
    {/* ts 5.0.2 to 7.0.2 show the 2nd diag here */}
    {bad}
    {/* comment */}
    {bad}
</div>;

// this was already correct
const single = <div>
    {/* comment */}
    {bad}
</div>;
