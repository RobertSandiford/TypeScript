//import { SyntaxKind } from "./types.js"

export function showStack(message: string = "") {
    try {
        throw new Error(message)
    } catch (e) {
        if (e instanceof Error) {
            const stack = e.stack
            if (stack !== undefined) {
                const lines = stack.split('\n')
                //console.log(
                console.log([(message + ' stack').trim(), ...lines.slice(2)].join('\n'))
            }
        }
    }
}
// export const specialWords = ['xx__closed', 'xx__open', 'readonly', 'XX__Readonly']
// export const specialSyntaxKinds = [SyntaxKind.ClosedKeyword, SyntaxKind.OpenKeyword, SyntaxKind.ReadonlyKeyword]