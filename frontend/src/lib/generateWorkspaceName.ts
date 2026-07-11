import {
  animals,
  colors,
  NumberDictionary,
  uniqueNamesGenerator,
} from 'unique-names-generator'

/** Same pattern Coder uses: color-animal-0..99 (e.g. yellow-bird-23). */
export function generateWorkspaceName(): string {
  const numberDictionary = NumberDictionary.generate({ min: 0, max: 99 })
  return uniqueNamesGenerator({
    dictionaries: [colors, animals, numberDictionary],
    separator: '-',
    length: 3,
    style: 'lowerCase',
  })
}
